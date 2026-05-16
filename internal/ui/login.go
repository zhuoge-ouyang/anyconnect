package ui

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Site 表示一个 VPN 站点
type Site struct {
	Name   string
	Server string
}

type LoginResult struct {
	SiteName string
	Server   string
	Username string
	Password string
	Remember bool
	OK       bool
}

func psSingleQuoted(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// ShowLoginDialog 弹出登录窗口。
func ShowLoginDialog(sites []Site, preferredSite, savedUsername string, rememberDefault bool) LoginResult {
	resultFile, err := os.CreateTemp("", "anyconnect-login-*.json")
	if err != nil {
		return LoginResult{}
	}
	resultPath := resultFile.Name()
	resultFile.Close()
	defer os.Remove(resultPath)

	// 拼接站点数据：name1|server1;name2|server2
	var sitesParts []string
	for _, s := range sites {
		sitesParts = append(sitesParts, s.Name+"|"+s.Server)
	}
	sitesStr := strings.Join(sitesParts, ";")
	sitesStr = psSingleQuoted(sitesStr)
	preferredSite = psSingleQuoted(preferredSite)
	savedUsername = psSingleQuoted(savedUsername)
	exePath, _ := os.Executable()
	iconPath := psSingleQuoted(filepath.Join(filepath.Dir(exePath), "app.ico"))
	escapedResultPath := psSingleQuoted(resultPath)
	rememberChecked := "$false"
	if rememberDefault {
		rememberChecked = "$true"
	}

	script := `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

[System.Windows.Forms.Application]::EnableVisualStyles()

$form = New-Object System.Windows.Forms.Form
$form.Text = 'AnyConnect 分流登录'
$form.Size = New-Object System.Drawing.Size(520, 470)
$form.StartPosition = 'CenterScreen'
$form.FormBorderStyle = 'FixedDialog'
$form.MaximizeBox = $false
$form.MinimizeBox = $false
$form.ShowInTaskbar = $true
$form.TopMost = $true
$form.BackColor = [System.Drawing.Color]::FromArgb(8, 13, 18)
$form.Font = New-Object System.Drawing.Font('Microsoft YaHei UI', 9)
$form.AutoScaleMode = [System.Windows.Forms.AutoScaleMode]::Dpi
$iconPath = '` + iconPath + `'
$resultPath = '` + escapedResultPath + `'
if (Test-Path $iconPath) {
    $form.Icon = New-Object System.Drawing.Icon($iconPath)
}

Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
public static class SplitTunnelNative {
    [DllImport("user32.dll")]
    public static extern bool SetForegroundWindow(IntPtr hWnd);
    [DllImport("user32.dll")]
    public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)]
    public static extern IntPtr SendMessage(IntPtr hWnd, int Msg, IntPtr wParam, string lParam);
}
"@

function New-UiFont($name, $size, $style) {
    return [System.Drawing.Font]::new($name, [single]$size, $style)
}

function Set-RoundedRegion($control, $radius) {
    $path = [System.Drawing.Drawing2D.GraphicsPath]::new()
    $w = $control.Width
    $h = $control.Height
    $r = [Math]::Min($radius, [Math]::Min($w, $h))
    $path.AddArc(0, 0, $r, $r, 180, 90)
    $path.AddArc($w - $r - 1, 0, $r, $r, 270, 90)
    $path.AddArc($w - $r - 1, $h - $r - 1, $r, $r, 0, 90)
    $path.AddArc(0, $h - $r - 1, $r, $r, 90, 90)
    $path.CloseFigure()
    $control.Region = [System.Drawing.Region]::new($path)
}

function Set-CueText($control, $text) {
    [SplitTunnelNative]::SendMessage($control.Handle, 0x1501, [IntPtr]1, $text) | Out-Null
}

$accent = [System.Drawing.Color]::FromArgb(29, 214, 255)
$accentDark = [System.Drawing.Color]::FromArgb(12, 122, 163)
$amber = [System.Drawing.Color]::FromArgb(245, 170, 32)
$ink = [System.Drawing.Color]::FromArgb(224, 240, 248)
$muted = [System.Drawing.Color]::FromArgb(138, 157, 171)
$panel = [System.Drawing.Color]::FromArgb(14, 22, 30)
$panelAlt = [System.Drawing.Color]::FromArgb(18, 28, 38)

$form.Add_Paint({
    param($sender, $e)
    $rect = $sender.ClientRectangle
    $brush = [System.Drawing.Drawing2D.LinearGradientBrush]::new(
        $rect,
        [System.Drawing.Color]::FromArgb(7, 13, 19),
        [System.Drawing.Color]::FromArgb(16, 25, 33),
        28
    )
    $e.Graphics.FillRectangle($brush, $rect)
    $brush.Dispose()

    $pen = New-Object System.Drawing.Pen([System.Drawing.Color]::FromArgb(24, 39, 50), 1)
    for ($x = 32; $x -lt 520; $x += 58) {
        $e.Graphics.DrawLine($pen, $x, 0, $x - 96, 470)
    }
    $pen.Dispose()
})

$form.Add_Shown({
    $form.WindowState = [System.Windows.Forms.FormWindowState]::Normal
    $form.TopMost = $true
    $form.Activate()
    $form.BringToFront()
    [SplitTunnelNative]::ShowWindow($form.Handle, 9) | Out-Null
    [SplitTunnelNative]::SetForegroundWindow($form.Handle) | Out-Null
    if ($txtUser.Text -eq '') {
        $txtUser.Focus()
    } else {
        $txtPass.Focus()
    }
})
$form.Add_Activated({
    $form.TopMost = $true
})

$focusTimer = New-Object System.Windows.Forms.Timer
$focusTimer.Interval = 220
$focusTicks = 0
$focusTimer.Add_Tick({
    $script:focusTicks++
    $form.TopMost = $true
    $form.BringToFront()
    [SplitTunnelNative]::ShowWindow($form.Handle, 9) | Out-Null
    [SplitTunnelNative]::SetForegroundWindow($form.Handle) | Out-Null
    if ($script:focusTicks -ge 10) {
        $focusTimer.Stop()
    }
})
$form.Add_Shown({ $focusTimer.Start() })

$shell = New-Object System.Windows.Forms.Panel
$shell.Location = New-Object System.Drawing.Point(20, 18)
$shell.Size = New-Object System.Drawing.Size(462, 382)
$shell.BackColor = [System.Drawing.Color]::FromArgb(11, 18, 25)
$form.Controls.Add($shell)
Set-RoundedRegion $shell 18

$topLine = New-Object System.Windows.Forms.Panel
$topLine.Location = New-Object System.Drawing.Point(0, 0)
$topLine.Size = New-Object System.Drawing.Size(462, 4)
$topLine.BackColor = $accent
$shell.Controls.Add($topLine)

$header = New-Object System.Windows.Forms.Panel
$header.Location = New-Object System.Drawing.Point(28, 28)
$header.Size = New-Object System.Drawing.Size(406, 82)
$header.BackColor = [System.Drawing.Color]::Transparent
$shell.Controls.Add($header)

$badge = New-Object System.Windows.Forms.Panel
$badge.Location = New-Object System.Drawing.Point(0, 0)
$badge.Size = New-Object System.Drawing.Size(70, 70)
$badge.BackColor = [System.Drawing.Color]::FromArgb(20, 31, 42)
$header.Controls.Add($badge)
Set-RoundedRegion $badge 18

if (Test-Path $iconPath) {
    $badgeIcon = New-Object System.Windows.Forms.PictureBox
    $badgeIcon.Location = New-Object System.Drawing.Point(8, 8)
    $badgeIcon.Size = New-Object System.Drawing.Size(54, 54)
    $badgeIcon.SizeMode = 'Zoom'
    $badgeIcon.Image = (New-Object System.Drawing.Icon($iconPath)).ToBitmap()
    $badge.Controls.Add($badgeIcon)
}

$eyebrow = New-Object System.Windows.Forms.Label
$eyebrow.Location = New-Object System.Drawing.Point(88, 0)
$eyebrow.Size = New-Object System.Drawing.Size(200, 18)
$eyebrow.Text = 'VPN ROUTE LOGIN'
$eyebrow.ForeColor = $amber
$eyebrow.Font = New-UiFont 'Bahnschrift' 8 ([System.Drawing.FontStyle]::Regular)
$header.Controls.Add($eyebrow)

$title = New-Object System.Windows.Forms.Label
$title.Location = New-Object System.Drawing.Point(86, 20)
$title.Size = New-Object System.Drawing.Size(230, 34)
$title.Text = '分流守卫'
$title.ForeColor = $ink
$title.Font = New-UiFont 'Microsoft YaHei UI' 20 ([System.Drawing.FontStyle]::Bold)
$header.Controls.Add($title)

$subtitle = New-Object System.Windows.Forms.Label
$subtitle.Location = New-Object System.Drawing.Point(88, 56)
$subtitle.Size = New-Object System.Drawing.Size(250, 22)
$subtitle.Text = 'AnyConnect 分流连接'
$subtitle.ForeColor = $muted
$subtitle.Font = New-UiFont 'Microsoft YaHei UI' 9 ([System.Drawing.FontStyle]::Regular)
$header.Controls.Add($subtitle)

$pill = New-Object System.Windows.Forms.Panel
$pill.Location = New-Object System.Drawing.Point(318, 19)
$pill.Size = New-Object System.Drawing.Size(88, 28)
$pill.BackColor = [System.Drawing.Color]::FromArgb(19, 43, 50)
$header.Controls.Add($pill)
Set-RoundedRegion $pill 14

$pillDot = New-Object System.Windows.Forms.Panel
$pillDot.Location = New-Object System.Drawing.Point(10, 10)
$pillDot.Size = New-Object System.Drawing.Size(8, 8)
$pillDot.BackColor = $accent
$pill.Controls.Add($pillDot)
Set-RoundedRegion $pillDot 8

$pillText = New-Object System.Windows.Forms.Label
$pillText.Location = New-Object System.Drawing.Point(24, 5)
$pillText.Size = New-Object System.Drawing.Size(60, 18)
$pillText.Text = '就绪'
$pillText.ForeColor = $ink
$pillText.Font = New-UiFont 'Bahnschrift' 8 ([System.Drawing.FontStyle]::Regular)
$pill.Controls.Add($pillText)

$content = New-Object System.Windows.Forms.Panel
$content.Location = New-Object System.Drawing.Point(28, 126)
$content.Size = New-Object System.Drawing.Size(406, 196)
$content.BackColor = $panel
$shell.Controls.Add($content)
Set-RoundedRegion $content 14

$lblSite = New-Object System.Windows.Forms.Label
$lblSite.Location = New-Object System.Drawing.Point(22, 18)
$lblSite.Size = New-Object System.Drawing.Size(360, 20)
$lblSite.Text = 'VPN 节点'
$lblSite.ForeColor = $muted
$lblSite.Font = New-UiFont 'Microsoft YaHei UI' 9 ([System.Drawing.FontStyle]::Regular)
$content.Controls.Add($lblSite)

$cboSite = New-Object System.Windows.Forms.ComboBox
$cboSite.Location = New-Object System.Drawing.Point(22, 42)
$cboSite.Size = New-Object System.Drawing.Size(362, 30)
$cboSite.DropDownStyle = 'DropDownList'
$cboSite.Font = New-UiFont 'Microsoft YaHei UI' 10 ([System.Drawing.FontStyle]::Regular)
$cboSite.BackColor = [System.Drawing.Color]::FromArgb(245, 248, 250)
$cboSite.ForeColor = [System.Drawing.Color]::FromArgb(18, 28, 38)

$sitesData = '` + sitesStr + `'
$preferredSite = '` + preferredSite + `'
$savedUsername = '` + savedUsername + `'
$sites = $sitesData -split ';'
$siteMap = @{}
foreach ($s in $sites) {
    $parts = $s -split '\|'
    $cboSite.Items.Add($parts[0]) | Out-Null
    $siteMap[$parts[0]] = $parts[1]
}
# 默认选中配置里的节点；没有匹配时优先海外节点。
$defaultIndex = 0
$foundDefault = $false
for ($i = 0; $i -lt $cboSite.Items.Count; $i++) {
    if ($preferredSite -ne '' -and $cboSite.Items[$i] -like "*$preferredSite*") {
        $defaultIndex = $i
        $foundDefault = $true
        break
    }
}
if (-not $foundDefault) {
    foreach ($hint in @('香港', '台湾', '日本', '韩国', '美国', '英国', '加拿大', '澳大利亚')) {
        for ($i = 0; $i -lt $cboSite.Items.Count; $i++) {
            if ($cboSite.Items[$i] -like "*$hint*") {
                $defaultIndex = $i
                $foundDefault = $true
                break
            }
        }
        if ($foundDefault) {
            break
        }
    }
}
if ($cboSite.Items.Count -gt 0) {
    $cboSite.SelectedIndex = $defaultIndex
}
$content.Controls.Add($cboSite)

$lblUser = New-Object System.Windows.Forms.Label
$lblUser.Location = New-Object System.Drawing.Point(22, 92)
$lblUser.Size = New-Object System.Drawing.Size(180, 20)
$lblUser.Text = '账号'
$lblUser.ForeColor = $muted
$content.Controls.Add($lblUser)

$txtUser = New-Object System.Windows.Forms.TextBox
$txtUser.Location = New-Object System.Drawing.Point(22, 116)
$txtUser.Size = New-Object System.Drawing.Size(171, 28)
$txtUser.Font = New-UiFont 'Bahnschrift' 11 ([System.Drawing.FontStyle]::Regular)
$txtUser.Text = $savedUsername
$txtUser.BorderStyle = 'FixedSingle'
$txtUser.BackColor = [System.Drawing.Color]::FromArgb(246, 249, 251)
$txtUser.ForeColor = [System.Drawing.Color]::FromArgb(15, 23, 42)
$content.Controls.Add($txtUser)

$lblPass = New-Object System.Windows.Forms.Label
$lblPass.Location = New-Object System.Drawing.Point(213, 92)
$lblPass.Size = New-Object System.Drawing.Size(180, 20)
$lblPass.Text = '密码'
$lblPass.ForeColor = $muted
$content.Controls.Add($lblPass)

$txtPass = New-Object System.Windows.Forms.TextBox
$txtPass.Location = New-Object System.Drawing.Point(213, 116)
$txtPass.Size = New-Object System.Drawing.Size(171, 28)
$txtPass.Font = New-UiFont 'Bahnschrift' 11 ([System.Drawing.FontStyle]::Regular)
$txtPass.UseSystemPasswordChar = $true
$txtPass.BorderStyle = 'FixedSingle'
$txtPass.BackColor = [System.Drawing.Color]::FromArgb(246, 249, 251)
$txtPass.ForeColor = [System.Drawing.Color]::FromArgb(15, 23, 42)
$content.Controls.Add($txtPass)
Set-CueText $txtUser 'VPN account'
Set-CueText $txtPass 'Password'

$chkRemember = New-Object System.Windows.Forms.CheckBox
$chkRemember.Location = New-Object System.Drawing.Point(22, 158)
$chkRemember.Size = New-Object System.Drawing.Size(260, 24)
$chkRemember.Text = '记住并下次自动连接'
$chkRemember.ForeColor = [System.Drawing.Color]::FromArgb(178, 197, 211)
$chkRemember.BackColor = $panel
$chkRemember.Checked = ` + rememberChecked + `
$content.Controls.Add($chkRemember)

$bottomBar = New-Object System.Windows.Forms.Panel
$bottomBar.Location = New-Object System.Drawing.Point(28, 338)
$bottomBar.Size = New-Object System.Drawing.Size(406, 1)
$bottomBar.BackColor = [System.Drawing.Color]::FromArgb(42, 62, 74)
$shell.Controls.Add($bottomBar)

$hint = New-Object System.Windows.Forms.Label
$hint.Location = New-Object System.Drawing.Point(30, 354)
$hint.Size = New-Object System.Drawing.Size(150, 26)
$hint.Text = '管理员模式'
$hint.ForeColor = [System.Drawing.Color]::FromArgb(120, 141, 154)
$hint.Font = New-UiFont 'Microsoft YaHei UI' 8 ([System.Drawing.FontStyle]::Regular)
$shell.Controls.Add($hint)

$btnOK = New-Object System.Windows.Forms.Button
$btnOK.Location = New-Object System.Drawing.Point(286, 350)
$btnOK.Size = New-Object System.Drawing.Size(148, 44)
$btnOK.Text = '连接 VPN'
$btnOK.FlatStyle = 'Flat'
$btnOK.BackColor = $accentDark
$btnOK.ForeColor = [System.Drawing.Color]::White
$btnOK.Font = New-UiFont 'Microsoft YaHei UI' 10 ([System.Drawing.FontStyle]::Bold)
$btnOK.FlatAppearance.BorderSize = 0
$btnOK.Cursor = [System.Windows.Forms.Cursors]::Hand
$btnOK.DialogResult = [System.Windows.Forms.DialogResult]::OK
$btnOK.Add_MouseEnter({ $btnOK.BackColor = [System.Drawing.Color]::FromArgb(18, 166, 210) })
$btnOK.Add_MouseLeave({ $btnOK.BackColor = $accentDark })
$btnOK.Add_Click({
    if ($txtUser.Text.Trim() -eq '' -or $txtPass.Text.Trim() -eq '') {
        [System.Windows.Forms.MessageBox]::Show($form, '请输入用户名和密码。', '需要凭据', 'OK', 'Warning') | Out-Null
        $form.DialogResult = [System.Windows.Forms.DialogResult]::None
    }
})
$form.AcceptButton = $btnOK
$shell.Controls.Add($btnOK)
Set-RoundedRegion $btnOK 12

$btnCancel = New-Object System.Windows.Forms.Button
$btnCancel.Location = New-Object System.Drawing.Point(174, 350)
$btnCancel.Size = New-Object System.Drawing.Size(96, 44)
$btnCancel.Text = '取消'
$btnCancel.FlatStyle = 'Flat'
$btnCancel.BackColor = [System.Drawing.Color]::FromArgb(28, 41, 52)
$btnCancel.ForeColor = [System.Drawing.Color]::FromArgb(206, 220, 230)
$btnCancel.FlatAppearance.BorderSize = 0
$btnCancel.Cursor = [System.Windows.Forms.Cursors]::Hand
$btnCancel.DialogResult = [System.Windows.Forms.DialogResult]::Cancel
$btnCancel.Add_MouseEnter({ $btnCancel.BackColor = [System.Drawing.Color]::FromArgb(37, 55, 68) })
$btnCancel.Add_MouseLeave({ $btnCancel.BackColor = [System.Drawing.Color]::FromArgb(28, 41, 52) })
$form.CancelButton = $btnCancel
$shell.Controls.Add($btnCancel)
Set-RoundedRegion $btnCancel 12

$result = $form.ShowDialog()

if ($result -eq [System.Windows.Forms.DialogResult]::OK) {
    $selectedName = $cboSite.SelectedItem.ToString()
    $selectedServer = $siteMap[$selectedName]
    $payload = [ordered]@{
        site_name = $selectedName
        server = $selectedServer
        username = $txtUser.Text
        password = $txtPass.Text
        remember = [bool]$chkRemember.Checked
    } | ConvertTo-Json -Compress
    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($resultPath, $payload, $utf8NoBom)
}
`

	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if err := cmd.Run(); err != nil {
		return LoginResult{}
	}

	data, err := os.ReadFile(resultPath)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return LoginResult{}
	}
	var payload struct {
		SiteName string `json:"site_name"`
		Server   string `json:"server"`
		Username string `json:"username"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return LoginResult{}
	}

	result := LoginResult{
		SiteName: strings.TrimSpace(payload.SiteName),
		Server:   strings.TrimSpace(payload.Server),
		Username: strings.TrimSpace(payload.Username),
		Password: strings.TrimSpace(payload.Password),
		Remember: payload.Remember,
		OK:       true,
	}
	if result.SiteName == "" || result.Server == "" || result.Username == "" || result.Password == "" {
		return LoginResult{}
	}

	return result
}
