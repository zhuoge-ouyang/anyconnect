# Desktop Contact Pages and Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the dashboard header icon with the WeChat QR code, turn the contact dialog into an in-place two-page flow, and keep the footer buttons fully visible at the minimum supported window size.

**Architecture:** Keep the existing Go-generated PowerShell/WinForms dashboard and its local static QR asset. Implement the contact flow with two WinForms panels whose `Visible` state is switched by button events, and make layout coordinates reliable by defining the main form in client-area units plus an anchored footer panel.

**Tech Stack:** Go 1.26, Windows PowerShell 5.1, WinForms, `System.Drawing`, Go `testing` package.

## Global Constraints

- Keep `ui-assets/wechat-contact-qr.png` as the only dashboard QR resource; do not redraw or substitute it.
- Do not modify VPN, route, split-tunnel, account, recharge, Android, or tray-menu behavior.
- Do not add dependencies or fallback images.
- Preserve the existing natural botanical background, warm paper cards, green text, and teal buttons.
- Do not commit or push; repository instructions require explicit user authorization for Git commits.

---

### Task 1: Lock the revised header and contact flow with failing tests

**Files:**
- Modify: `internal/ui/dashboard_test.go`
- Test: `internal/ui/dashboard_test.go`

**Interfaces:**
- Consumes: `dashboardScript(snapshotPath, commandDir, iconPath string) string`
- Produces: regression assertions for `$headerQrBox`, `$contactInfoPage`, `$contactQrPage`, `$btnShowQr`, and `$btnBack`

- [ ] **Step 1: Replace the existing contact test assertions and add a header QR test**

Add these tests to `internal/ui/dashboard_test.go`:

```go
func TestDashboardHeaderUsesContactQRCode(t *testing.T) {
	script := dashboardScript(`C:\Temp\state.json`, `C:\Temp\commands`, `C:\Temp\app.ico`)

	for _, want := range []string{
		"$headerQrBox = [System.Windows.Forms.PictureBox]::new()",
		"$headerQrBox.SizeMode = [System.Windows.Forms.PictureBoxSizeMode]::Zoom",
		"$headerQrImage = [System.Drawing.Image]::FromFile($contactQrPath)",
		"$headerQrBox.Image = $headerQrImage",
		"$headerQrError.Text = '二维码加载失败'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("dashboard header QR script missing %q", want)
		}
	}
	if strings.Contains(script, "$iconBox.Image = ([System.Drawing.Icon]::new($iconPath)).ToBitmap()") {
		t.Fatal("dashboard header should not render the application icon")
	}
}

func TestDashboardContactButtonOpensTwoPageDialog(t *testing.T) {
	script := dashboardScript(`C:\Temp\state.json`, `C:\Temp\commands`, `C:\Temp\app.ico`)

	for _, want := range []string{
		"$contactInfoPage = [System.Windows.Forms.Panel]::new()",
		"$contactQrPage = [System.Windows.Forms.Panel]::new()",
		"有问题、建议或需要定制，可以联系作者。",
		"作者：卓哥",
		"微信号：ai_creater99",
		"需要安卓客户端请联系作者。",
		"$btnShowQr = New-Button",
		"'查看微信二维码'",
		"$btnShowQr.Add_Click({",
		"$contactInfoPage.Visible = $false",
		"$contactQrPage.Visible = $true",
		"$btnBack.Add_Click({",
		"$contactInfoPage.Visible = $true",
		"$contactQrPage.Visible = $false",
		"$btnContact.Add_Click({ Show-ContactDialog $form })",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("two-page contact dialog script missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run the focused tests and verify RED**

Run:

```powershell
go test ./internal/ui -run 'Dashboard(HeaderUsesContactQRCode|ContactButtonOpensTwoPageDialog)' -count=1
```

Expected: FAIL because `$headerQrBox`, `$contactInfoPage`, and `$contactQrPage` do not exist in the current script.

---

### Task 2: Implement the header QR and two-page contact dialog

**Files:**
- Modify: `internal/ui/dashboard.go:341-405`
- Modify: `internal/ui/dashboard.go:483-498`
- Test: `internal/ui/dashboard_test.go`

**Interfaces:**
- Consumes: `$contactQrPath`, `New-Label`, `New-Button`, `Set-RoundedRegion`
- Produces: `$headerQrImage`, `$contactInfoPage`, `$contactQrPage`; both QR images are disposed on window close

- [ ] **Step 1: Replace `Show-ContactDialog` with one modal window containing two pages**

Use a `460 x 390` client area. Create `$contactInfoPage` and `$contactQrPage` with identical bounds, add the required labels and buttons to their respective panels, and initialize visibility as follows:

```powershell
$contactInfoPage.Visible = $true
$contactQrPage.Visible = $false
$btnShowQr.Add_Click({
    $contactInfoPage.Visible = $false
    $contactQrPage.Visible = $true
})
$btnBack.Add_Click({
    $contactInfoPage.Visible = $true
    $contactQrPage.Visible = $false
})
```

The first page must use `Set-Clipboard -Value 'ai_creater99'`; the second page must load `$contactQrPath` into a `230 x 230` `PictureBox` with `Zoom`. On load failure, hide only the QR box and show `二维码加载失败`. After `ShowDialog`, clear and dispose `$qrImage`.

- [ ] **Step 2: Replace the header application icon with a QR thumbnail**

Create an 82 x 82 white host panel at `(18, 18)`, then a 70 x 70 `$headerQrBox` at `(6, 6)`. Load `$contactQrPath` using `Image.FromFile`, use `Zoom`, and show a centered error label if loading fails. Keep the form icon assignment unchanged so the title bar still uses the application icon.

Store the loaded image in `$headerQrImage` and dispose it in `$form.Add_FormClosed`:

```powershell
if ($null -ne $headerQrImage) {
    $headerQrBox.Image = $null
    $headerQrImage.Dispose()
}
```

Increase the title width so `AnyConnect 分流管理台` is fully visible without moving the status pill.

- [ ] **Step 3: Run the focused tests and verify GREEN**

Run:

```powershell
go test ./internal/ui -run 'Dashboard(HeaderUsesContactQRCode|ContactButtonOpensTwoPageDialog)' -count=1
```

Expected: PASS.

- [ ] **Step 4: Run PowerShell parsing and initialization tests**

Run:

```powershell
go test ./internal/ui -run 'DashboardScript(Parses|InitializesOnWindowsPowerShell)' -count=1
```

Expected: PASS with no PowerShell parse or WinForms initialization errors.

---

### Task 3: Lock and implement the non-clipping footer layout

**Files:**
- Modify: `internal/ui/dashboard_test.go`
- Modify: `internal/ui/dashboard.go:448-457`
- Modify: `internal/ui/dashboard.go:597-602`
- Test: `internal/ui/dashboard_test.go`

**Interfaces:**
- Consumes: existing `$form`, `New-Panel`, and `New-Button`
- Produces: `$footerActions`, `$btnRechargeHelp`, and `$btnContact`, anchored to the bottom-right client area

- [ ] **Step 1: Write the failing footer layout test**

Add:

```go
func TestDashboardFooterButtonsStayInsideClientArea(t *testing.T) {
	script := dashboardScript(`C:\Temp\state.json`, `C:\Temp\commands`, `C:\Temp\app.ico`)

	for _, want := range []string{
		"$form.ClientSize = [System.Drawing.Size]::new(864, 640)",
		"$footerActions = [System.Windows.Forms.Panel]::new()",
		"$footerActions.Anchor = [System.Windows.Forms.AnchorStyles]::Bottom -bor [System.Windows.Forms.AnchorStyles]::Right",
		"$btnRechargeHelp = New-Button 0 0 126 38 '充值说明'",
		"$btnContact = New-Button 136 0 116 38 '联系作者'",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("dashboard footer layout missing %q", want)
		}
	}
	if strings.Contains(script, "$btnContact = New-Button 724 574 116 34 '联系作者'") {
		t.Fatal("dashboard contact button still uses the clipped form-level coordinate")
	}
}
```

- [ ] **Step 2: Run the footer test and verify RED**

Run:

```powershell
go test ./internal/ui -run DashboardFooterButtonsStayInsideClientArea -count=1
```

Expected: FAIL because the main form uses `Size` and the buttons are direct children at `y=574`.

- [ ] **Step 3: Implement client-area sizing and an anchored footer panel**

Set the main form to:

```powershell
$form.ClientSize = [System.Drawing.Size]::new(864, 640)
$form.MinimumSize = [System.Drawing.Size]::new(820, 640)
```

Replace the two direct form buttons with:

```powershell
$footerActions = [System.Windows.Forms.Panel]::new()
$footerActions.Location = [System.Drawing.Point]::new(588, 584)
$footerActions.Size = [System.Drawing.Size]::new(252, 38)
$footerActions.BackColor = [System.Drawing.Color]::Transparent
$footerActions.Anchor = [System.Windows.Forms.AnchorStyles]::Bottom -bor [System.Windows.Forms.AnchorStyles]::Right
$form.Controls.Add($footerActions)

$btnRechargeHelp = New-Button 0 0 126 38 '充值说明'
$btnRechargeHelp.Add_Click({ Show-RechargeDialog $form })
$footerActions.Controls.Add($btnRechargeHelp)
$btnContact = New-Button 136 0 116 38 '联系作者'
$btnContact.Add_Click({ Show-ContactDialog $form })
$footerActions.Controls.Add($btnContact)
```

- [ ] **Step 4: Run the footer test and verify GREEN**

Run:

```powershell
go test ./internal/ui -run DashboardFooterButtonsStayInsideClientArea -count=1
```

Expected: PASS.

---

### Task 4: Verify the dashboard visually and rebuild deliverables

**Files:**
- Verify: `internal/ui/dashboard.go`
- Verify: `internal/ui/dashboard_test.go`
- Regenerate: `bin/anyconnect-split.exe`
- Regenerate: `artifacts/AnyConnectSplitTunnelSetup.exe`

**Interfaces:**
- Consumes: completed dashboard script and existing `tools/package.ps1`
- Produces: tested desktop executable and installer containing the revised dashboard and QR asset

- [ ] **Step 1: Run all UI and repository tests**

Run:

```powershell
go test ./internal/ui -count=1
go test ./... -count=1
```

Expected: all packages PASS.

- [ ] **Step 2: Rebuild the desktop program and installer**

Exit the currently running dashboard process if it locks UI assets, then run:

```powershell
powershell -ExecutionPolicy Bypass -File .\tools\package.ps1
```

Expected: `bin/anyconnect-split.exe` and `artifacts/AnyConnectSplitTunnelSetup.exe` are recreated successfully, and the package script confirms `wechat-contact-qr.png` is present.

- [ ] **Step 3: Run the actual dashboard and visually inspect the requested states**

Launch the rebuilt program, open the management dashboard, and confirm:

- the header QR is the user's WeChat QR and remains scannable;
- the title is not ellipsized;
- both footer buttons are fully visible;
- page one matches the approved text and button order;
- `查看微信二维码` shows page two in the same dialog;
- `返回` restores page one;
- the page-two QR is fully visible and scannable.

- [ ] **Step 4: Run final artifact and diff checks**

Run:

```powershell
git diff --check
Get-FileHash -Algorithm SHA256 .\bin\anyconnect-split.exe, .\artifacts\AnyConnectSplitTunnelSetup.exe
```

Expected: `git diff --check` exits 0 and both files have non-empty SHA-256 hashes.
