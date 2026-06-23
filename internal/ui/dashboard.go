package ui

import (
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/user/anyconnect-split/internal/dashboard"
)

const dashboardWindowTitle = "AnyConnect 分流管理台"

var (
	dashboardMu  sync.Mutex
	dashboardCmd *exec.Cmd
)

func ShowDashboard(store *dashboard.Store, iconPath string) error {
	dashboardMu.Lock()
	defer dashboardMu.Unlock()

	if dashboardCmd != nil && dashboardCmd.ProcessState == nil {
		focusDashboardWindow()
		return nil
	}

	script := dashboardScript(store.SnapshotPath(), store.CommandDir(), iconPath)
	cmd := exec.Command("powershell", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		return err
	}
	dashboardCmd = cmd
	go func() {
		_ = cmd.Wait()
		dashboardMu.Lock()
		if dashboardCmd == cmd {
			dashboardCmd = nil
		}
		dashboardMu.Unlock()
	}()
	return nil
}

func focusDashboardWindow() {
	title := psSingleQuoted(dashboardWindowTitle)
	script := `
Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
public static class DashboardFocusNative {
    [DllImport("user32.dll", CharSet=CharSet.Unicode)]
    public static extern IntPtr FindWindow(string className, string windowName);
    [DllImport("user32.dll")]
    public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
    [DllImport("user32.dll")]
    public static extern bool SetForegroundWindow(IntPtr hWnd);
    [DllImport("user32.dll")]
    public static extern bool SetWindowPos(IntPtr hWnd, IntPtr hWndInsertAfter, int X, int Y, int cx, int cy, uint uFlags);
}
"@
$hwnd = [DashboardFocusNative]::FindWindow($null, '` + title + `')
if ($hwnd -ne [IntPtr]::Zero) {
    [DashboardFocusNative]::ShowWindow($hwnd, 9) | Out-Null
    $HWND_TOPMOST = [IntPtr]::new(-1)
    $HWND_NOTOPMOST = [IntPtr]::new(-2)
    $SWP_NOMOVE = 0x0002
    $SWP_NOSIZE = 0x0001
    $SWP_SHOWWINDOW = 0x0040
    [DashboardFocusNative]::SetWindowPos($hwnd, $HWND_TOPMOST, 0, 0, 0, 0, $SWP_NOMOVE -bor $SWP_NOSIZE -bor $SWP_SHOWWINDOW) | Out-Null
    [DashboardFocusNative]::SetWindowPos($hwnd, $HWND_NOTOPMOST, 0, 0, 0, 0, $SWP_NOMOVE -bor $SWP_NOSIZE -bor $SWP_SHOWWINDOW) | Out-Null
    [DashboardFocusNative]::SetForegroundWindow($hwnd) | Out-Null
}
`
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	_ = cmd.Start()
}

func dashboardScript(snapshotPath, commandDir, iconPath string) string {
	snapshotPath = psSingleQuoted(snapshotPath)
	commandDir = psSingleQuoted(commandDir)
	iconPath = psSingleQuoted(iconPath)
	title := psSingleQuoted(dashboardWindowTitle)
	script := `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()

Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
public static class DashboardForeNative {
    [DllImport("user32.dll")]
    public static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);
    [DllImport("user32.dll")]
    public static extern bool SetForegroundWindow(IntPtr hWnd);
    [DllImport("user32.dll")]
    public static extern bool SetWindowPos(IntPtr hWnd, IntPtr hWndInsertAfter, int X, int Y, int cx, int cy, uint uFlags);
}
"@

$snapshotPath = '__SNAPSHOT_PATH__'
$commandDir = '__COMMAND_DIR__'
$iconPath = '__ICON_PATH__'
New-Item -ItemType Directory -Force -Path $commandDir | Out-Null

function New-Font($size, $style = [System.Drawing.FontStyle]::Regular) {
    return [System.Drawing.Font]::new('Microsoft YaHei UI', [single]$size, $style)
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

function New-Label($x, $y, $w, $h, $text, $size, $color, $style = [System.Drawing.FontStyle]::Regular) {
    $label = [System.Windows.Forms.Label]::new()
    $label.Location = [System.Drawing.Point]::new($x, $y)
    $label.Size = [System.Drawing.Size]::new($w, $h)
    $label.Text = $text
    $label.Font = New-Font $size $style
    $label.ForeColor = $color
    $label.BackColor = [System.Drawing.Color]::Transparent
    $label.AutoEllipsis = $true
    return $label
}

function New-Button($x, $y, $w, $h, $text, $primary = $false) {
    $button = [System.Windows.Forms.Button]::new()
    $button.Location = [System.Drawing.Point]::new($x, $y)
    $button.Size = [System.Drawing.Size]::new($w, $h)
    $button.Text = $text
    $button.Font = New-Font 9 ([System.Drawing.FontStyle]::Bold)
    $button.FlatStyle = 'Flat'
    $button.FlatAppearance.BorderSize = 0
    $button.Cursor = [System.Windows.Forms.Cursors]::Hand
    if ($primary) {
		$button.BackColor = [System.Drawing.Color]::FromArgb(25, 137, 166)
		$button.ForeColor = [System.Drawing.Color]::White
		$button.Add_MouseEnter({ param($sender, $e) $sender.BackColor = [System.Drawing.Color]::FromArgb(18, 111, 139) })
		$button.Add_MouseLeave({ param($sender, $e) $sender.BackColor = [System.Drawing.Color]::FromArgb(25, 137, 166) })
	} else {
		$button.BackColor = [System.Drawing.Color]::FromArgb(235, 244, 248)
		$button.ForeColor = [System.Drawing.Color]::FromArgb(34, 55, 68)
		$button.FlatAppearance.BorderColor = [System.Drawing.Color]::FromArgb(203, 218, 226)
		$button.Add_MouseEnter({ param($sender, $e) $sender.BackColor = [System.Drawing.Color]::FromArgb(224, 238, 244) })
		$button.Add_MouseLeave({ param($sender, $e) $sender.BackColor = [System.Drawing.Color]::FromArgb(235, 244, 248) })
	}
    Set-RoundedRegion $button 10
    return $button
}

function New-Panel($x, $y, $w, $h, $color) {
    $panel = [System.Windows.Forms.Panel]::new()
    $panel.Location = [System.Drawing.Point]::new($x, $y)
    $panel.Size = [System.Drawing.Size]::new($w, $h)
    $panel.BackColor = $color
    Set-RoundedRegion $panel 18
    return $panel
}

function Write-Command($action, $enabled = $null) {
    $payload = [ordered]@{
        action = $action
        created_at = [DateTime]::UtcNow.ToString('o')
    }
    if ($null -ne $enabled) {
        $payload.enabled = [bool]$enabled
    }
    $name = ('{0}-{1}.json' -f [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds(), [Guid]::NewGuid().ToString('N'))
    $path = Join-Path $commandDir $name
    $json = $payload | ConvertTo-Json -Compress
    $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText($path, $json, $utf8NoBom)
    $script:commandHint.Text = '已发送：' + $action
}

function Format-Time($value) {
    if ($null -eq $value -or [string]::IsNullOrWhiteSpace([string]$value)) {
        return '未记录'
    }
    try {
        $dt = [datetime]$value
        if ($dt.Year -lt 2000) { return '未更新' }
        return $dt.ToLocalTime().ToString('yyyy-MM-dd HH:mm')
    } catch {
        return '未记录'
    }
}

$ink = [System.Drawing.Color]::FromArgb(24, 44, 56)
$muted = [System.Drawing.Color]::FromArgb(96, 116, 128)
$line = [System.Drawing.Color]::FromArgb(211, 225, 232)
$cyan = [System.Drawing.Color]::FromArgb(25, 137, 166)
$green = [System.Drawing.Color]::FromArgb(25, 154, 116)
$amber = [System.Drawing.Color]::FromArgb(196, 132, 28)
$paper = [System.Drawing.Color]::FromArgb(248, 252, 253)
$glass = [System.Drawing.Color]::FromArgb(239, 247, 250)

$form = [System.Windows.Forms.Form]::new()
$form.Text = '__WINDOW_TITLE__'
$form.Size = [System.Drawing.Size]::new(880, 640)
$form.StartPosition = 'CenterScreen'
$form.MinimumSize = [System.Drawing.Size]::new(820, 600)
$form.BackColor = [System.Drawing.Color]::FromArgb(230, 239, 244)
$form.Font = New-Font 9
$form.AutoScaleMode = [System.Windows.Forms.AutoScaleMode]::Dpi
$form.ShowInTaskbar = $true
$form.TopMost = $true
if (Test-Path $iconPath) {
    $form.Icon = [System.Drawing.Icon]::new($iconPath)
}

$form.Add_Paint({
    param($sender, $e)
    $rect = $sender.ClientRectangle
    if ($rect.Width -le 0 -or $rect.Height -le 0) { return }
    $brush = [System.Drawing.Drawing2D.LinearGradientBrush]::new(
        $rect,
        [System.Drawing.Color]::FromArgb(244, 250, 252),
        [System.Drawing.Color]::FromArgb(218, 232, 240),
        35
    )
    $e.Graphics.FillRectangle($brush, $rect)
    $brush.Dispose()
})

$header = New-Panel 24 22 816 118 $paper
$form.Controls.Add($header)

if (Test-Path $iconPath) {
    $iconBox = [System.Windows.Forms.PictureBox]::new()
    $iconBox.Location = [System.Drawing.Point]::new(24, 24)
    $iconBox.Size = [System.Drawing.Size]::new(70, 70)
    $iconBox.SizeMode = 'Zoom'
    $iconBox.Image = ([System.Drawing.Icon]::new($iconPath)).ToBitmap()
    $header.Controls.Add($iconBox)
}

$title = New-Label 112 24 280 34 'AnyConnect 分流管理台' 18 $ink ([System.Drawing.FontStyle]::Bold)
$header.Controls.Add($title)
$subtitle = New-Label 114 62 340 24 '控制、线路和诊断状态' 9 $muted
$header.Controls.Add($subtitle)

$statusPill = New-Panel 520 28 260 58 ([System.Drawing.Color]::FromArgb(230, 246, 250))
$header.Controls.Add($statusPill)
$statusDot = [System.Windows.Forms.Panel]::new()
$statusDot.Location = [System.Drawing.Point]::new(18, 21)
$statusDot.Size = [System.Drawing.Size]::new(14, 14)
$statusDot.BackColor = $cyan
$statusPill.Controls.Add($statusDot)
Set-RoundedRegion $statusDot 14
$statusLabel = New-Label 42 12 198 22 '状态：初始化中...' 10 $ink ([System.Drawing.FontStyle]::Bold)
$statusPill.Controls.Add($statusLabel)
$siteLabel = New-Label 42 34 198 18 '当前站点：未连接' 8 $muted
$statusPill.Controls.Add($siteLabel)

$control = New-Panel 24 158 392 408 $paper
$form.Controls.Add($control)
$control.Controls.Add((New-Label 24 22 180 26 '常用操作' 13 $ink ([System.Drawing.FontStyle]::Bold)))
$commandHint = New-Label 208 26 150 20 '等待操作' 8 $muted
$control.Controls.Add($commandHint)

$btnDisconnect = New-Button 24 66 158 42 '断开 VPN'
$btnReconnect = New-Button 202 66 158 42 '重新连接' $true
$btnCodex = New-Button 24 124 158 42 'Codex 稳定线路'
$btnRestore = New-Button 202 124 158 42 '恢复常用线路'
$btnUpdate = New-Button 24 182 158 42 '更新 IP 数据库'
$btnLog = New-Button 202 182 158 42 '查看日志'
$btnContact = New-Button 24 240 158 42 '联系作者'
$btnQuit = New-Button 202 240 158 42 '退出程序'
@($btnDisconnect,$btnReconnect,$btnCodex,$btnRestore,$btnUpdate,$btnLog,$btnContact,$btnQuit) | ForEach-Object { $control.Controls.Add($_) }

$chkSplit = [System.Windows.Forms.CheckBox]::new()
$chkSplit.Location = [System.Drawing.Point]::new(26, 318)
$chkSplit.Size = [System.Drawing.Size]::new(150, 26)
$chkSplit.Text = '启用分流'
$chkSplit.ForeColor = $ink
$chkSplit.BackColor = $paper
$chkSplit.Font = New-Font 10
$control.Controls.Add($chkSplit)

$chkAuto = [System.Windows.Forms.CheckBox]::new()
$chkAuto.Location = [System.Drawing.Point]::new(202, 318)
$chkAuto.Size = [System.Drawing.Size]::new(150, 26)
$chkAuto.Text = '开机自启'
$chkAuto.ForeColor = $ink
$chkAuto.BackColor = $paper
$chkAuto.Font = New-Font 10
$control.Controls.Add($chkAuto)

$diagnostics = New-Panel 440 158 400 408 $paper
$form.Controls.Add($diagnostics)
$diagnostics.Controls.Add((New-Label 24 22 180 26 '运行诊断' 13 $ink ([System.Drawing.FontStyle]::Bold)))

$diagNames = @('分流模式','后端模式','路由数量','IP 库更新','IPv4 网关','IPv6 网关','IPv6 分流','最近错误')
$diagLabels = @{}
$y = 66
foreach ($name in $diagNames) {
    $diagnostics.Controls.Add((New-Label 24 $y 92 24 $name 9 $muted))
    $value = New-Label 126 $y 236 24 '读取中...' 9 $ink ([System.Drawing.FontStyle]::Bold)
    $diagnostics.Controls.Add($value)
    $diagLabels[$name] = $value
    $sep = [System.Windows.Forms.Panel]::new()
    $sep.Location = [System.Drawing.Point]::new(24, $y + 32)
    $sep.Size = [System.Drawing.Size]::new(340, 1)
    $sep.BackColor = $line
    $diagnostics.Controls.Add($sep)
    $y += 42
}

$script:hydrating = $false

function Refresh-State {
    if (!(Test-Path $snapshotPath)) { return }
    try {
        $raw = Get-Content -LiteralPath $snapshotPath -Raw -Encoding UTF8
        if ([string]::IsNullOrWhiteSpace($raw)) { return }
        $state = $raw | ConvertFrom-Json
    } catch {
        return
    }

    $script:hydrating = $true
    $status = [string]$state.status_text
    if ([string]::IsNullOrWhiteSpace($status)) { $status = '状态：未知' }
    $statusLabel.Text = $status
    $site = [string]$state.current_site
    if ([string]::IsNullOrWhiteSpace($site)) { $site = '未连接' }
    $siteLabel.Text = '当前站点：' + $site
    $split = [bool]$state.split_tunnel_enabled
    $auto = [bool]$state.auto_start_enabled
    $chkSplit.Checked = $split
    $chkAuto.Checked = $auto
    $diagLabels['分流模式'].Text = $(if ($split) { '已启用' } else { '未启用' })
    $backend = [string]$state.backend
    if ([string]::IsNullOrWhiteSpace($backend)) { $backend = '未连接' }
    $diagLabels['后端模式'].Text = $backend
    $diagLabels['路由数量'].Text = [string]$state.route_count
    $diagLabels['IP 库更新'].Text = Format-Time $state.last_ipdb_update
    $ipv4 = [string]$state.original_gateway
    if ([string]::IsNullOrWhiteSpace($ipv4)) { $ipv4 = '未检测' }
    if ([int]$state.original_interface -gt 0) { $ipv4 = $ipv4 + ' / if ' + [string]$state.original_interface }
    $diagLabels['IPv4 网关'].Text = $ipv4
    $ipv6 = [string]$state.original_ipv6_gateway
    if ([string]::IsNullOrWhiteSpace($ipv6)) { $ipv6 = '未检测' }
    if ([int]$state.original_ipv6_interface_index -gt 0) { $ipv6 = $ipv6 + ' / if ' + [string]$state.original_ipv6_interface_index }
    $diagLabels['IPv6 网关'].Text = $ipv6
    $diagLabels['IPv6 分流'].Text = $(if ([bool]$state.ipv6_split_enabled) { '已启用' } else { '未启用' })
    $errText = [string]$state.last_error
    if ([string]::IsNullOrWhiteSpace($errText)) { $errText = '无' }
    $diagLabels['最近错误'].Text = $errText
    if ($status -like '*错误*' -or -not [string]::IsNullOrWhiteSpace([string]$state.last_error)) {
        $statusDot.BackColor = [System.Drawing.Color]::FromArgb(210, 76, 76)
    } elseif ($status -like '*正在*' -or $status -like '*初始化*') {
        $statusDot.BackColor = $amber
    } elseif ($split) {
        $statusDot.BackColor = $green
    } else {
        $statusDot.BackColor = $cyan
    }
    $script:hydrating = $false
}

$btnDisconnect.Add_Click({ Write-Command 'disconnect' })
$btnReconnect.Add_Click({ Write-Command 'reconnect' })
$btnCodex.Add_Click({ Write-Command 'codex_mode' })
$btnRestore.Add_Click({ Write-Command 'restore_normal' })
$btnUpdate.Add_Click({ Write-Command 'update_ipdb' })
$btnLog.Add_Click({ Write-Command 'view_log' })
$btnContact.Add_Click({ Write-Command 'contact_author' })
$btnQuit.Add_Click({ Write-Command 'quit' })
$chkSplit.Add_CheckedChanged({ if (-not $script:hydrating) { Write-Command 'toggle_split' $chkSplit.Checked } })
$chkAuto.Add_CheckedChanged({ if (-not $script:hydrating) { Write-Command 'toggle_autostart' $chkAuto.Checked } })

$timer = [System.Windows.Forms.Timer]::new()
$timer.Interval = 1000
$timer.Add_Tick({ Refresh-State })

$unTopTimer = [System.Windows.Forms.Timer]::new()
$unTopTimer.Interval = 400
$unTopTimer.Add_Tick({
    $form.TopMost = $false
    $unTopTimer.Stop()
})

$form.Add_Shown({
    Refresh-State
    $timer.Start()
    $form.WindowState = [System.Windows.Forms.FormWindowState]::Normal
    $form.Activate()
    $form.BringToFront()
    [DashboardForeNative]::ShowWindow($form.Handle, 9) | Out-Null
    [DashboardForeNative]::SetForegroundWindow($form.Handle) | Out-Null
    $unTopTimer.Start()
})
$form.Add_FormClosed({
    $timer.Stop()
    $unTopTimer.Stop()
})

[void]$form.ShowDialog()
`
	script = strings.ReplaceAll(script, "__SNAPSHOT_PATH__", snapshotPath)
	script = strings.ReplaceAll(script, "__COMMAND_DIR__", commandDir)
	script = strings.ReplaceAll(script, "__ICON_PATH__", iconPath)
	script = strings.ReplaceAll(script, "__WINDOW_TITLE__", title)
	return script
}
