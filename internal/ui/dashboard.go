package ui

import (
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/user/anyconnect-split/internal/dashboard"
)

const dashboardWindowTitle = "AnyConnect 分流管理台"

type dashboardLaunchDecision int

const (
	dashboardLaunchStart dashboardLaunchDecision = iota
	dashboardLaunchFocus
	dashboardLaunchWait
)

var (
	dashboardMu                       sync.Mutex
	dashboardCmd                      *exec.Cmd
	dashboardUser32                   = syscall.NewLazyDLL("user32.dll")
	dashboardEnumWindows              = dashboardUser32.NewProc("EnumWindows")
	dashboardGetWindowThreadProcessID = dashboardUser32.NewProc("GetWindowThreadProcessId")
	dashboardGetWindowText            = dashboardUser32.NewProc("GetWindowTextW")
	dashboardIsIconic                 = dashboardUser32.NewProc("IsIconic")
	dashboardShowWindow               = dashboardUser32.NewProc("ShowWindow")
	dashboardSetForegroundWindow      = dashboardUser32.NewProc("SetForegroundWindow")
	dashboardSetWindowPos             = dashboardUser32.NewProc("SetWindowPos")
)

func decideDashboardLaunch(processRunning, windowExists bool) dashboardLaunchDecision {
	if !processRunning {
		return dashboardLaunchStart
	}
	if windowExists {
		return dashboardLaunchFocus
	}
	return dashboardLaunchWait
}

func dashboardWindowForProcess(processID int) uintptr {
	if processID <= 0 {
		return 0
	}
	var found uintptr
	callback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var windowProcessID uint32
		dashboardGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&windowProcessID)))
		if int(windowProcessID) != processID {
			return 1
		}

		text := make([]uint16, 256)
		length, _, _ := dashboardGetWindowText.Call(
			hwnd,
			uintptr(unsafe.Pointer(&text[0])),
			uintptr(len(text)),
		)
		if length == 0 || syscall.UTF16ToString(text) != dashboardWindowTitle {
			return 1
		}
		found = hwnd
		return 0
	})
	dashboardEnumWindows.Call(callback, 0)
	return found
}

func dashboardWindowIsMinimized(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	minimized, _, _ := dashboardIsIconic.Call(hwnd)
	return minimized != 0
}

func focusDashboardWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	const (
		swRestore     = 9
		swpNoSize     = 0x0001
		swpNoMove     = 0x0002
		swpShowWindow = 0x0040
	)
	hwndTopmost := ^uintptr(0)
	hwndNoTopmost := ^uintptr(1)
	flags := uintptr(swpNoSize | swpNoMove | swpShowWindow)

	dashboardShowWindow.Call(hwnd, swRestore)
	dashboardSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, flags)
	dashboardSetWindowPos.Call(hwnd, hwndNoTopmost, 0, 0, 0, 0, flags)
	dashboardSetForegroundWindow.Call(hwnd)
}

func ShowDashboard(store *dashboard.Store, iconPath string) error {
	dashboardMu.Lock()
	defer dashboardMu.Unlock()

	processRunning := dashboardCmd != nil && dashboardCmd.ProcessState == nil
	var hwnd uintptr
	if processRunning {
		hwnd = dashboardWindowForProcess(dashboardCmd.Process.Pid)
	}
	switch decideDashboardLaunch(processRunning, hwnd != 0) {
	case dashboardLaunchFocus:
		focusDashboardWindow(hwnd)
		return nil
	case dashboardLaunchWait:
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
    [DllImport("user32.dll", CharSet=CharSet.Unicode)]
    public static extern IntPtr SendMessage(IntPtr hWnd, int msg, IntPtr wParam, string lParam);
}
"@

$snapshotPath = '__SNAPSHOT_PATH__'
$commandDir = '__COMMAND_DIR__'
$iconPath = '__ICON_PATH__'
$dashboardBgPath = Join-Path ([System.IO.Path]::GetDirectoryName($iconPath)) 'ui-assets\desktop-dashboard-bg.png'
$contactQrPath = Join-Path ([System.IO.Path]::GetDirectoryName($iconPath)) 'ui-assets\wechat-contact-qr.png'
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

function Show-RechargeDialog($owner) {
    $dialog = [System.Windows.Forms.Form]::new()
    $dialog.Text = '账号充值说明'
    $dialog.Size = [System.Drawing.Size]::new(680, 650)
    $dialog.StartPosition = 'CenterParent'
    $dialog.FormBorderStyle = 'FixedDialog'
    $dialog.MaximizeBox = $false
    $dialog.MinimizeBox = $false
    $dialog.BackColor = [System.Drawing.Color]::FromArgb(255, 253, 242)
    $dialog.Font = New-Font 9
    if (Test-Path $iconPath) {
        $dialog.Icon = [System.Drawing.Icon]::new($iconPath)
    }

    $title = New-Label 24 18 610 34 '线上购买与充值流程' 16 ([System.Drawing.Color]::FromArgb(48, 84, 53)) ([System.Drawing.FontStyle]::Bold)
    $dialog.Controls.Add($title)

    $infoText = @'
【一、套餐价格】

40 元 / 1 个月
180 元 / 半年
240 元 / 1 年
400 元 / 2 年
520 元 / 3 年

【二、购买须知】

• 支持苹果、安卓、Mac、Windows 等主流平台。
• 不限制安装绑定设备数量，但同时使用数量不超过 2 台。
• 合理使用不限流量。
• 不提供试用，不支持退款。购买前请先向推荐人详细了解产品。

【三、注册账号】

1. 打开浏览器（建议使用设备自带浏览器，不要使用微信或百度浏览器）。
   复制下面的注册链接并打开：

   https://vip90123.com/signup

2. 注册时填写推荐码：

   Sm3xWXkUif

   如果有其他推荐人的推荐码，请填写对方的推荐码；没有则填写上面的推荐码。

3. 填写邮箱后，记得点击“发送”按钮。
   如果收不到验证码，请检查邮箱是否填写正确，并查看垃圾邮件，
   同时将验证邮件标记为“这不是垃圾邮件”。推荐使用 QQ、163 等国内邮箱。

【四、充值并购买套餐】

4. 注册并登录后，点击余额旁边的“充值”，按提示完成充值。
   请根据要购买的套餐充值对应金额。
   微信或百度浏览器可能会卡住，请使用设备自带浏览器或其他浏览器。

5. 充值完成后会自动进入套餐购买页面。
   如果没有自动进入，请在网站首页找到套餐，点击右侧“购买”按钮。
   选择账号 → 点击“下一步” → 选择套餐时长 → 点击“下一步”。
   请认真核对账号和套餐后再提交。提交后余额变为 0 属于正常情况。

【五、下载和安装】

6. 回到网站首页，点击左上角“冲浪俱乐部”。
   在页面下方“下载专区”中，点击对应设备平台图标，打开安装设置说明。
   请务必完整阅读设置说明，安装完成后还需要继续进行后续设置。

【六、安装其他设备】

7. 如需安装其他设备，登录网站首页，在“下载专区”点击对应设备图标，
   按说明完成安装和设置，然后使用同一个账号登录。
   不要重复注册账号或子账号，也不要重复充值、购买套餐。
'@

    $box = [System.Windows.Forms.TextBox]::new()
    $box.Location = [System.Drawing.Point]::new(24, 62)
    $box.Size = [System.Drawing.Size]::new(616, 476)
    $box.Multiline = $true
    $box.ScrollBars = [System.Windows.Forms.ScrollBars]::Vertical
    $box.ReadOnly = $true
    $box.Text = [regex]::Replace($infoText, "\r?\n", [Environment]::NewLine)
    $box.BackColor = [System.Drawing.Color]::FromArgb(255, 255, 249)
    $box.ForeColor = [System.Drawing.Color]::FromArgb(48, 65, 40)
    $box.Font = New-Font 10
    $dialog.Controls.Add($box)

    $btnCopyLink = New-Button 24 556 126 38 '复制注册链接'
    $btnCopyLink.Add_Click({
        Set-Clipboard -Value 'https://vip90123.com/signup'
        [System.Windows.Forms.MessageBox]::Show($dialog, '注册链接已复制。', '已复制', 'OK', 'Information') | Out-Null
    })
    $dialog.Controls.Add($btnCopyLink)

    $btnCopyCode = New-Button 160 556 126 38 '复制推荐码'
    $btnCopyCode.Add_Click({
        Set-Clipboard -Value 'Sm3xWXkUif'
        [System.Windows.Forms.MessageBox]::Show($dialog, '推荐码已复制。', '已复制', 'OK', 'Information') | Out-Null
    })
    $dialog.Controls.Add($btnCopyCode)

    $btnClose = New-Button 514 556 126 38 '知道了' $true
    $btnClose.DialogResult = [System.Windows.Forms.DialogResult]::OK
    $dialog.AcceptButton = $btnClose
    $dialog.Controls.Add($btnClose)

    [void]$dialog.ShowDialog($owner)
}

function Show-ContactDialog($owner) {
    $dialog = [System.Windows.Forms.Form]::new()
    $dialog.Text = '联系作者'
    $dialog.ClientSize = [System.Drawing.Size]::new(460, 390)
    $dialog.StartPosition = 'CenterParent'
    $dialog.FormBorderStyle = 'FixedDialog'
    $dialog.MaximizeBox = $false
    $dialog.MinimizeBox = $false
    $dialog.BackColor = [System.Drawing.Color]::FromArgb(255, 253, 242)
    $dialog.Font = New-Font 9
    if (Test-Path $iconPath) {
        $dialog.Icon = [System.Drawing.Icon]::new($iconPath)
    }

    $contactInfoPage = [System.Windows.Forms.Panel]::new()
    $contactInfoPage.Dock = [System.Windows.Forms.DockStyle]::Fill
    $contactInfoPage.BackColor = $dialog.BackColor
    $dialog.Controls.Add($contactInfoPage)

    $infoTitle = New-Label 24 22 412 38 '联系作者' 18 ([System.Drawing.Color]::FromArgb(48, 84, 53)) ([System.Drawing.FontStyle]::Bold)
    $contactInfoPage.Controls.Add($infoTitle)
    $infoHint = New-Label 24 70 412 30 '有问题、建议或需要定制，可以联系作者。' 10 ([System.Drawing.Color]::FromArgb(96, 116, 78))
    $contactInfoPage.Controls.Add($infoHint)
    $author = New-Label 24 120 412 28 '作者：卓哥' 11 ([System.Drawing.Color]::FromArgb(48, 84, 53)) ([System.Drawing.FontStyle]::Bold)
    $contactInfoPage.Controls.Add($author)
    $wechat = New-Label 24 160 412 28 '微信号：ai_creater99' 11 ([System.Drawing.Color]::FromArgb(48, 84, 53)) ([System.Drawing.FontStyle]::Bold)
    $contactInfoPage.Controls.Add($wechat)
    $androidHint = New-Label 24 204 412 28 '需要安卓客户端请联系作者。' 10 ([System.Drawing.Color]::FromArgb(184, 132, 48)) ([System.Drawing.FontStyle]::Bold)
    $contactInfoPage.Controls.Add($androidHint)

    $btnCopy = New-Button 24 314 120 42 '复制微信号' $true
    $btnCopy.Add_Click({
        Set-Clipboard -Value 'ai_creater99'
        [System.Windows.Forms.MessageBox]::Show($dialog, '微信号已复制。', '已复制', 'OK', 'Information') | Out-Null
    })
    $contactInfoPage.Controls.Add($btnCopy)

    $btnShowQr = New-Button 154 314 164 42 '查看微信二维码'
    $contactInfoPage.Controls.Add($btnShowQr)

    $btnInfoClose = New-Button 328 314 108 42 '关闭'
    $btnInfoClose.DialogResult = [System.Windows.Forms.DialogResult]::Cancel
    $dialog.CancelButton = $btnInfoClose
    $contactInfoPage.Controls.Add($btnInfoClose)

    $contactQrPage = [System.Windows.Forms.Panel]::new()
    $contactQrPage.Dock = [System.Windows.Forms.DockStyle]::Fill
    $contactQrPage.BackColor = $dialog.BackColor
    $dialog.Controls.Add($contactQrPage)

    $qrTitle = New-Label 24 16 412 36 '微信二维码' 17 ([System.Drawing.Color]::FromArgb(48, 84, 53)) ([System.Drawing.FontStyle]::Bold)
    $qrTitle.TextAlign = [System.Drawing.ContentAlignment]::MiddleCenter
    $contactQrPage.Controls.Add($qrTitle)
    $qrHint = New-Label 24 52 412 24 '微信扫码添加作者' 10 ([System.Drawing.Color]::FromArgb(96, 116, 78))
    $qrHint.TextAlign = [System.Drawing.ContentAlignment]::MiddleCenter
    $contactQrPage.Controls.Add($qrHint)

    $qrBox = [System.Windows.Forms.PictureBox]::new()
    $qrBox.Location = [System.Drawing.Point]::new(115, 78)
    $qrBox.Size = [System.Drawing.Size]::new(230, 230)
    $qrBox.BackColor = [System.Drawing.Color]::White
    $qrBox.BorderStyle = [System.Windows.Forms.BorderStyle]::FixedSingle
    $qrBox.SizeMode = [System.Windows.Forms.PictureBoxSizeMode]::Zoom
    $contactQrPage.Controls.Add($qrBox)

    $qrImage = $null
    try {
        if (!(Test-Path -LiteralPath $contactQrPath)) {
            throw 'Contact QR code is missing.'
        }
        $qrImage = [System.Drawing.Image]::FromFile($contactQrPath)
        $qrBox.Image = $qrImage
    } catch {
        $qrBox.Visible = $false
        $errorLabel = New-Label 24 174 412 30 '二维码加载失败' 11 ([System.Drawing.Color]::FromArgb(174, 70, 55)) ([System.Drawing.FontStyle]::Bold)
        $errorLabel.TextAlign = [System.Drawing.ContentAlignment]::MiddleCenter
        $contactQrPage.Controls.Add($errorLabel)
    }

    $btnBack = New-Button 115 326 108 42 '返回'
    $contactQrPage.Controls.Add($btnBack)
    $btnQrClose = New-Button 237 326 108 42 '关闭'
    $btnQrClose.DialogResult = [System.Windows.Forms.DialogResult]::Cancel
    $contactQrPage.Controls.Add($btnQrClose)

    $btnShowQr.Add_Click({
        $contactInfoPage.Visible = $false
        $contactQrPage.Visible = $true
    })
    $btnBack.Add_Click({
        $contactInfoPage.Visible = $true
        $contactQrPage.Visible = $false
    })
    $contactInfoPage.Visible = $true
    $contactQrPage.Visible = $false

    [void]$dialog.ShowDialog($owner)
    if ($null -ne $qrImage) {
        $qrBox.Image = $null
        $qrImage.Dispose()
    }
}

function Write-Command($action, $enabled = $null, $value = $null) {
    $payload = [ordered]@{
        action = $action
        created_at = [DateTime]::UtcNow.ToString('o')
    }
    if ($null -ne $enabled) {
        $payload.enabled = [bool]$enabled
    }
    if ($null -ne $value -and [string]::IsNullOrWhiteSpace([string]$value) -eq $false) {
        $payload.value = [string]$value
    }
    $name = ('{0}-{1}.json' -f [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds(), [Guid]::NewGuid().ToString('N'))
    $path = Join-Path $commandDir $name
    $json = $payload | ConvertTo-Json -Compress
    $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText($path, $json, $utf8NoBom)
    $script:commandHint.Text = '已发送：' + $action
}

function Show-WhitelistDialog($owner, $state) {
    $dialog = [System.Windows.Forms.Form]::new()
    $dialog.Text = '管理国外白名单（这些目标走 VPN）'
    $dialog.ClientSize = [System.Drawing.Size]::new(620, 430)
    $dialog.StartPosition = 'CenterParent'
    $dialog.FormBorderStyle = 'FixedDialog'
    $dialog.MaximizeBox = $false
    $dialog.MinimizeBox = $false
    $dialog.BackColor = [System.Drawing.Color]::FromArgb(255, 253, 242)
    if (Test-Path $iconPath) { $dialog.Icon = [System.Drawing.Icon]::new($iconPath) }
    $dialog.Controls.Add((New-Label 20 16 570 28 '国外白名单（域名、IP/CIDR）' 14 ([System.Drawing.Color]::FromArgb(42,76,48)) ([System.Drawing.FontStyle]::Bold)))
    $dialog.Controls.Add((New-Label 20 48 570 24 '列表中的目标通过 VPN；国内应用仍按当前分流模式直连。' 9 ([System.Drawing.Color]::FromArgb(96,116,78))))
    $list = [System.Windows.Forms.ListBox]::new()
    $list.Location = [System.Drawing.Point]::new(20, 82)
    $list.Size = [System.Drawing.Size]::new(580, 230)
    $list.Font = New-Font 10
    foreach ($v in @($state.foreign_domains)) { if (-not [string]::IsNullOrWhiteSpace([string]$v)) { [void]$list.Items.Add(('域名 | ' + $v)) } }
    foreach ($v in @($state.foreign_cidrs)) { if (-not [string]::IsNullOrWhiteSpace([string]$v)) { [void]$list.Items.Add(('IP/CIDR | ' + $v)) } }
    $dialog.Controls.Add($list)
    $input = [System.Windows.Forms.TextBox]::new()
    $input.Location = [System.Drawing.Point]::new(20, 328)
    $input.Size = [System.Drawing.Size]::new(286, 30)
    $input.Font = New-Font 10
    $dialog.Controls.Add($input)
    [void][DashboardForeNative]::SendMessage($input.Handle, 0x1501, [IntPtr]::Zero, '输入域名或 IP/CIDR')
    $addDomain = New-Button 316 326 90 34 '添加域名'
    $addIP = New-Button 414 326 90 34 '添加 IP'
    $remove = New-Button 510 326 90 34 '删除选中'
    @($addDomain,$addIP,$remove) | ForEach-Object { $dialog.Controls.Add($_) }
    $addDomain.Add_Click({
        $v = $input.Text.Trim(); if ($v -eq '') { return }
        Write-Command 'add_foreign_domain' $null $v
        [void]$list.Items.Add(('域名 | ' + $v)); $input.Clear()
    })
    $addIP.Add_Click({
        $v = $input.Text.Trim(); if ($v -eq '') { return }
        Write-Command 'add_foreign_cidr' $null $v
        [void]$list.Items.Add(('IP/CIDR | ' + $v)); $input.Clear()
    })
    $remove.Add_Click({
        if ($list.SelectedIndex -lt 0) { return }
        $entry = [string]$list.SelectedItem
        if ($entry.StartsWith('域名 | ')) { Write-Command 'remove_foreign_domain' $null $entry.Substring(5) }
        elseif ($entry.StartsWith('IP/CIDR | ')) { Write-Command 'remove_foreign_cidr' $null $entry.Substring(10) }
        $list.Items.RemoveAt($list.SelectedIndex)
    })
    $close = New-Button 480 378 120 36 '完成' $true
    $close.DialogResult = [System.Windows.Forms.DialogResult]::OK
    $dialog.Controls.Add($close)
    [void]$dialog.ShowDialog($owner)
}

function Show-RecommendationDialog($owner, $state) {
    $dialog = [System.Windows.Forms.Form]::new()
    $dialog.Text = '智能选线结果'
    $dialog.ClientSize = [System.Drawing.Size]::new(520, 330)
    $dialog.StartPosition = 'CenterParent'
    $dialog.FormBorderStyle = 'FixedDialog'
    $dialog.ControlBox = $false
    $dialog.KeyPreview = $true
    $dialog.BackColor = [System.Drawing.Color]::FromArgb(255,253,242)
    $dialog.Controls.Add((New-Label 24 20 470 34 ('推荐：' + [string]$state.smart_candidate) 16 ([System.Drawing.Color]::FromArgb(42,76,48)) ([System.Drawing.FontStyle]::Bold)))
    $nl = [Environment]::NewLine
    $details = "ChatGPT/OpenAI：$($state.smart_successes)/$($state.smart_attempts)" + $nl + "中位耗时：$($state.smart_median_ms) ms　最慢：$($state.smart_slowest_ms) ms" + $nl + "出口：$($state.smart_exit_ip) / $($state.smart_exit_region)"
    $dialog.Controls.Add((New-Label 24 76 470 110 $details 10 ([System.Drawing.Color]::FromArgb(42,76,48))))
    $count = 20
    $countLabel = New-Label 24 198 470 28 '20 秒后自动采用推荐线路' 10 ([System.Drawing.Color]::FromArgb(184,132,48)) ([System.Drawing.FontStyle]::Bold)
    $dialog.Controls.Add($countLabel)
    $accept = New-Button 24 248 220 42 '立即采用' $true
    $restore = New-Button 268 248 220 42 '恢复原线路'
    $dialog.Controls.Add($accept); $dialog.Controls.Add($restore)
    $decision = $false
    $accept.Add_Click({ $script:recommendDecision = 'accept'; Write-Command 'smart_select_accept'; $dialog.Close() })
    $restore.Add_Click({ $script:recommendDecision = 'restore'; Write-Command 'smart_select_restore'; $dialog.Close() })
    $dialog.Add_KeyDown({ param($sender,$e) if ($e.KeyCode -eq [System.Windows.Forms.Keys]::Escape) { $e.SuppressKeyPress = $true } })
    $tick = [System.Windows.Forms.Timer]::new(); $tick.Interval = 1000
    $tick.Add_Tick({
        $count--; $countLabel.Text = "$count 秒后自动采用推荐线路"
        if ($count -le 0) { $tick.Stop(); Write-Command 'smart_select_accept'; $dialog.Close() }
    })
    $dialog.Add_Shown({ $tick.Start() })
    $dialog.Add_FormClosed({ $tick.Stop(); $tick.Dispose() })
    [void]$dialog.ShowDialog($owner)
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

$ink = [System.Drawing.Color]::FromArgb(42, 76, 48)
$muted = [System.Drawing.Color]::FromArgb(96, 116, 78)
$line = [System.Drawing.Color]::FromArgb(211, 226, 190)
$cyan = [System.Drawing.Color]::FromArgb(78, 145, 124)
$green = [System.Drawing.Color]::FromArgb(79, 158, 94)
$amber = [System.Drawing.Color]::FromArgb(184, 132, 48)
$paper = [System.Drawing.Color]::FromArgb(255, 253, 242)
$glass = [System.Drawing.Color]::FromArgb(241, 249, 230)

$form = [System.Windows.Forms.Form]::new()
$form.Text = '__WINDOW_TITLE__'
$form.ClientSize = [System.Drawing.Size]::new(864, 640)
$form.StartPosition = 'CenterScreen'
$form.MinimumSize = [System.Drawing.Size]::new(820, 640)
$form.BackColor = [System.Drawing.Color]::FromArgb(250, 244, 220)
$form.Font = New-Font 9
$form.AutoScaleMode = [System.Windows.Forms.AutoScaleMode]::Dpi
$form.ShowInTaskbar = $true
$form.TopMost = $true
if (Test-Path $iconPath) {
    $form.Icon = [System.Drawing.Icon]::new($iconPath)
}
$script:hasDashboardBackground = $false
if (Test-Path $dashboardBgPath) {
    $form.BackgroundImage = [System.Drawing.Image]::FromFile($dashboardBgPath)
    $form.BackgroundImageLayout = [System.Windows.Forms.ImageLayout]::Stretch
    $script:hasDashboardBackground = $true
}

$form.Add_Paint({
    param($sender, $e)
    if ($script:hasDashboardBackground) { return }
    $rect = $sender.ClientRectangle
    if ($rect.Width -le 0 -or $rect.Height -le 0) { return }
    $brush = [System.Drawing.Drawing2D.LinearGradientBrush]::new(
        $rect,
        [System.Drawing.Color]::FromArgb(250, 244, 220),
        [System.Drawing.Color]::FromArgb(219, 239, 211),
        35
    )
    $e.Graphics.FillRectangle($brush, $rect)
    $brush.Dispose()
})

$header = New-Panel 24 22 816 118 $paper
$form.Controls.Add($header)

$headerQrHost = New-Panel 18 18 82 82 ([System.Drawing.Color]::White)
$header.Controls.Add($headerQrHost)
$headerQrBox = [System.Windows.Forms.PictureBox]::new()
$headerQrBox.Location = [System.Drawing.Point]::new(6, 6)
$headerQrBox.Size = [System.Drawing.Size]::new(70, 70)
$headerQrBox.SizeMode = [System.Windows.Forms.PictureBoxSizeMode]::Zoom
$headerQrBox.BackColor = [System.Drawing.Color]::White
$headerQrHost.Controls.Add($headerQrBox)
$headerQrImage = $null
try {
    if (!(Test-Path -LiteralPath $contactQrPath)) {
        throw 'Contact QR code is missing.'
    }
    $headerQrImage = [System.Drawing.Image]::FromFile($contactQrPath)
    $headerQrBox.Image = $headerQrImage
} catch {
    $headerQrBox.Visible = $false
    $headerQrError = [System.Windows.Forms.Label]::new()
    $headerQrError.Dock = [System.Windows.Forms.DockStyle]::Fill
    $headerQrError.Text = '二维码加载失败'
    $headerQrError.Font = New-Font 7 ([System.Drawing.FontStyle]::Bold)
    $headerQrError.ForeColor = [System.Drawing.Color]::FromArgb(174, 70, 55)
    $headerQrError.TextAlign = [System.Drawing.ContentAlignment]::MiddleCenter
    $headerQrHost.Controls.Add($headerQrError)
}

$title = New-Label 112 24 380 34 'AnyConnect 分流管理台' 18 $ink ([System.Drawing.FontStyle]::Bold)
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
$btnCodex = New-Button 24 124 336 42 'ChatGPT/Codex 智能选线'
$btnRestore = New-Button 24 124 336 42 '恢复常用线路'
$btnRestore.Visible = $false
$btnUpdate = New-Button 24 182 158 42 '更新 IP 数据库'
$btnLog = New-Button 202 182 158 42 '查看日志'
$btnQuit = New-Button 24 240 336 42 '退出程序'
@($btnDisconnect,$btnReconnect,$btnCodex,$btnRestore,$btnUpdate,$btnLog,$btnQuit) | ForEach-Object { $control.Controls.Add($_) }

$splitAlwaysOn = New-Label 26 318 98 26 '分流已常驻' 10 $green ([System.Drawing.FontStyle]::Bold)
$control.Controls.Add($splitAlwaysOn)

$chkAuto = [System.Windows.Forms.CheckBox]::new()
$chkAuto.Location = [System.Drawing.Point]::new(126, 318)
$chkAuto.Size = [System.Drawing.Size]::new(96, 26)
$chkAuto.Text = '开机自启'
$chkAuto.ForeColor = $ink
$chkAuto.BackColor = $paper
$chkAuto.Font = New-Font 10
$control.Controls.Add($chkAuto)

$cmbSplitMode = [System.Windows.Forms.ComboBox]::new()
$cmbSplitMode.Location = [System.Drawing.Point]::new(224, 315)
$cmbSplitMode.Size = [System.Drawing.Size]::new(136, 30)
$cmbSplitMode.DropDownStyle = [System.Windows.Forms.ComboBoxStyle]::DropDownList
$cmbSplitMode.Font = New-Font 9
[void]$cmbSplitMode.Items.Add('国内直连优先')
[void]$cmbSplitMode.Items.Add('国外 VPN 优先')
$cmbSplitMode.SelectedIndex = 0
$control.Controls.Add($cmbSplitMode)

$btnWhitelist = New-Button 24 354 336 38 '管理国外白名单（这些目标走 VPN）'
$control.Controls.Add($btnWhitelist)

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

$script:hydrating = $false
$script:lastSmartResult = ''
$script:latestState = $null

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
    $script:latestState = $state
    $status = [string]$state.status_text
    if ([string]::IsNullOrWhiteSpace($status)) { $status = '状态：未知' }
    $statusLabel.Text = $status
    $site = [string]$state.current_site
    if ([string]::IsNullOrWhiteSpace($site)) { $site = '未连接' }
    $siteLabel.Text = '当前站点：' + $site
    $auto = [bool]$state.auto_start_enabled
    $codexMode = $false
    if ($null -ne $state.codex_mode_active) { $codexMode = [bool]$state.codex_mode_active }
    $btnCodex.Visible = -not $codexMode
    $btnRestore.Visible = $codexMode
    $smartState = [string]$state.smart_state
    $smartRunning = @('running','restoring','current_healthy','recommendation') -contains $smartState
    $btnDisconnect.Enabled = -not $smartRunning
    $btnReconnect.Enabled = -not $smartRunning
    $btnRestore.Enabled = -not $smartRunning
    $btnUpdate.Enabled = -not $smartRunning
    $btnWhitelist.Enabled = -not $smartRunning
    $cmbSplitMode.Enabled = -not $smartRunning
    if ($smartState -eq 'running' -or $smartState -eq 'restoring') { $btnCodex.Visible = $true; $btnRestore.Visible = $false; $btnCodex.Text = '取消智能选线' }
    else { $btnCodex.Text = 'ChatGPT/Codex 智能选线' }
    $resultID = [string]$state.smart_result_id
    if ($resultID -ne '' -and $resultID -ne $script:lastSmartResult) {
        $script:lastSmartResult = $resultID
        if ($smartState -eq 'current_healthy') {
            $answer = [System.Windows.Forms.MessageBox]::Show($form, ([string]$state.smart_message + [Environment]::NewLine + [Environment]::NewLine + '是否继续深度检测其他线路？'), '当前线路健康', 'YesNo', 'Information')
            if ($answer -eq [System.Windows.Forms.DialogResult]::Yes) { Write-Command 'smart_select_continue' } else { Write-Command 'smart_select_cancel' }
        } elseif ($smartState -eq 'recommendation') { Show-RecommendationDialog $form $state }
    }
    $chkAuto.Checked = $auto
    $splitMode = [string]$state.split_mode
    $modeIndex = $(if ($splitMode -eq 'foreign_direct') { 1 } else { 0 })
    if ($cmbSplitMode.SelectedIndex -ne $modeIndex) { $cmbSplitMode.SelectedIndex = $modeIndex }
    $modeText = $(if ($modeIndex -eq 1) { '国外 VPN 优先' } else { '国内直连优先' })
    $diagLabels['分流模式'].Text = '已启用 / ' + $modeText
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
    } else {
        $statusDot.BackColor = $green
    }
    $script:hydrating = $false
}

$btnDisconnect.Add_Click({ Write-Command 'disconnect' })
$btnReconnect.Add_Click({ Write-Command 'reconnect' })
$btnCodex.Add_Click({
    if ($null -ne $script:latestState -and @('running','restoring') -contains [string]$script:latestState.smart_state) { Write-Command 'smart_select_cancel'; return }
    $answer = [System.Windows.Forms.MessageBox]::Show($form, '智能诊断最多 60 秒。检测期间国外连接和 ChatGPT 响应可能短暂中断，国内直连应用通常不受影响。是否开始？', '开始智能选线', 'YesNo', 'Warning')
    if ($answer -eq [System.Windows.Forms.DialogResult]::Yes) { Write-Command 'codex_mode' }
})
$btnRestore.Add_Click({ Write-Command 'restore_normal' })
$btnUpdate.Add_Click({ Write-Command 'update_ipdb' })
$btnLog.Add_Click({ Write-Command 'view_log' })
$btnWhitelist.Add_Click({ if ($null -ne $script:latestState) { Show-WhitelistDialog $form $script:latestState } })
$btnQuit.Add_Click({ Write-Command 'quit' })
$chkAuto.Add_CheckedChanged({ if (-not $script:hydrating) { Write-Command 'toggle_autostart' $chkAuto.Checked } })
$cmbSplitMode.Add_SelectedIndexChanged({
    if ($script:hydrating -or $cmbSplitMode.SelectedIndex -lt 0) { return }
    $mode = $(if ($cmbSplitMode.SelectedIndex -eq 1) { 'foreign_direct' } else { 'domestic_direct' })
    Write-Command 'set_split_mode' $null $mode
})

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
    if ($null -ne $headerQrImage) {
        $headerQrBox.Image = $null
        $headerQrImage.Dispose()
    }
})

[void]$form.ShowDialog()
`
	script = strings.ReplaceAll(script, "__SNAPSHOT_PATH__", snapshotPath)
	script = strings.ReplaceAll(script, "__COMMAND_DIR__", commandDir)
	script = strings.ReplaceAll(script, "__ICON_PATH__", iconPath)
	script = strings.ReplaceAll(script, "__WINDOW_TITLE__", title)
	return script
}
