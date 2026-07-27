package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/user/anyconnect-split/internal/tray"
)

func TestDashboardLaunchDecisionHandlesWindowLifecycle(t *testing.T) {
	tests := []struct {
		name           string
		processRunning bool
		windowExists   bool
		want           dashboardLaunchDecision
	}{
		{name: "no process launches dashboard", want: dashboardLaunchStart},
		{name: "live window is focused", processRunning: true, windowExists: true, want: dashboardLaunchFocus},
		{name: "process without window is allowed to finish starting", processRunning: true, want: dashboardLaunchWait},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decideDashboardLaunch(tt.processRunning, tt.windowExists); got != tt.want {
				t.Fatalf("decideDashboardLaunch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDashboardHeaderUsesContactQRCode(t *testing.T) {
	script := dashboardScript(
		`C:\Temp\anyconnect-dashboard-state.json`,
		`C:\Temp\anyconnect-dashboard-commands`,
		`C:\Temp\app.ico`,
	)

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
	script := dashboardScript(
		`C:\Temp\anyconnect-dashboard-state.json`,
		`C:\Temp\anyconnect-dashboard-commands`,
		`C:\Temp\app.ico`,
	)

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

	for _, unwanted := range []string{
		"$btnContact.Add_Click({ Write-Command 'contact_author' })",
		"$btnContact = New-Button 24 240 158 42 '联系作者'",
	} {
		if strings.Contains(script, unwanted) {
			t.Fatalf("dashboard contact script should not contain %q", unwanted)
		}
	}
}

func TestDashboardFooterButtonsStayInsideClientArea(t *testing.T) {
	script := dashboardScript(
		`C:\Temp\anyconnect-dashboard-state.json`,
		`C:\Temp\anyconnect-dashboard-commands`,
		`C:\Temp\app.ico`,
	)

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

func TestFocusDashboardWindowRestoresMinimizedProcessWindow(t *testing.T) {
	script := `
Add-Type -AssemblyName System.Windows.Forms
$form = [System.Windows.Forms.Form]::new()
$form.Text = 'AnyConnect 分流管理台'
$form.ShowInTaskbar = $true
$form.Add_Shown({ $form.WindowState = [System.Windows.Forms.FormWindowState]::Minimized })
[void]$form.ShowDialog()
`
	cmd := exec.Command("powershell", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start minimized dashboard fixture: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	deadline := time.Now().Add(10 * time.Second)
	var hwnd uintptr
	for time.Now().Before(deadline) {
		hwnd = dashboardWindowForProcess(cmd.Process.Pid)
		if hwnd != 0 && dashboardWindowIsMinimized(hwnd) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if hwnd == 0 {
		t.Fatal("dashboard fixture did not create a window")
	}
	if !dashboardWindowIsMinimized(hwnd) {
		t.Fatal("dashboard fixture window was not minimized")
	}

	focusDashboardWindow(hwnd)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !dashboardWindowIsMinimized(hwnd) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("focusDashboardWindow did not restore the minimized dashboard")
}

func TestDashboardScriptParses(t *testing.T) {
	script := dashboardScript(
		`C:\Temp\anyconnect-dashboard-state.json`,
		`C:\Temp\anyconnect-dashboard-commands`,
		`C:\Temp\app.ico`,
	)

	scriptPath := filepath.Join(t.TempDir(), "dashboard.ps1")
	if err := writeUTF16LE(scriptPath, script); err != nil {
		t.Fatalf("write dashboard script: %v", err)
	}

	parserScript := `
$ErrorActionPreference = 'Stop'
$tokens = $null
$parseErrors = $null
[System.Management.Automation.Language.Parser]::ParseFile(__SCRIPT_PATH__, [ref]$tokens, [ref]$parseErrors) | Out-Null
if ($parseErrors.Count -gt 0) {
    $parseErrors | ForEach-Object { Write-Output $_.Message }
    exit 1
}
`
	parserScript = strings.ReplaceAll(parserScript, "__SCRIPT_PATH__", strconv.Quote(scriptPath))
	cmd := exec.Command("powershell", "-NoProfile", "-Command", parserScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dashboard PowerShell does not parse: %v\n%s", err, string(output))
	}
}

func TestDashboardScriptInitializesOnWindowsPowerShell(t *testing.T) {
	script := dashboardScript(
		filepath.Join(t.TempDir(), "anyconnect-dashboard-state.json"),
		filepath.Join(t.TempDir(), "anyconnect-dashboard-commands"),
		filepath.Join(t.TempDir(), "app.ico"),
	)
	script = strings.Replace(script, "[void]$form.ShowDialog()", "$form.Dispose()", 1)

	cmd := exec.Command("powershell", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dashboard PowerShell failed during WinForms initialization: %v\n%s", err, string(output))
	}
}

func TestDashboardScriptShowsAndClosesOnWindowsPowerShell(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "anyconnect-dashboard-state.json")
	iconPath := filepath.Join(dir, "app.ico")
	if err := os.WriteFile(iconPath, tray.AppIcon, 0644); err != nil {
		t.Fatalf("write dashboard icon: %v", err)
	}
	snapshot := `{
  "status_text": "状态：TUN 分流已启用",
  "current_site": "03.国内专线-深圳节点",
  "split_tunnel_enabled": true,
  "split_mode": "domestic_direct",
  "auto_start_enabled": true,
  "backend": "openconnect_tun",
  "route_count": 0,
  "codex_mode_active": false,
  "last_ipdb_update": "2026-07-04T20:18:25.9652961+08:00",
  "original_gateway": "192.168.3.1",
  "original_interface": 22,
  "original_ipv6_gateway": "fe80::ded4:44ff:fef1:c5fb",
  "original_ipv6_interface_index": 22,
  "ipv6_split_enabled": false,
  "last_error": ""
}`
	if err := os.WriteFile(snapshotPath, []byte(snapshot), 0644); err != nil {
		t.Fatalf("write dashboard snapshot: %v", err)
	}

	script := dashboardScript(
		snapshotPath,
		filepath.Join(dir, "anyconnect-dashboard-commands"),
		iconPath,
	)
	script = strings.Replace(
		script,
		"[void]$form.ShowDialog()",
		"$form.Add_Shown({ $form.BeginInvoke([Action]{ $form.Close() }) | Out-Null })\n[void]$form.ShowDialog()",
		1,
	)

	cmd := exec.Command("powershell", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dashboard PowerShell failed while showing the WinForms window: %v\n%s", err, string(output))
	}
}

func TestDashboardScriptDoesNotDrawDecorativeDiagonalLines(t *testing.T) {
	script := dashboardScript(
		`C:\Temp\anyconnect-dashboard-state.json`,
		`C:\Temp\anyconnect-dashboard-commands`,
		`C:\Temp\app.ico`,
	)

	if strings.Contains(script, ".DrawLine(") {
		t.Fatal("dashboard script should not draw decorative background lines")
	}
}

func writeUTF16LE(path, text string) error {
	encoded := utf16.Encode([]rune(text))
	data := make([]byte, 2+len(encoded)*2)
	data[0], data[1] = 0xff, 0xfe
	for i, r := range encoded {
		data[2+i*2] = byte(r)
		data[3+i*2] = byte(r >> 8)
	}
	return os.WriteFile(path, data, 0644)
}
