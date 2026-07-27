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

func loginDefaultSite(sites []Site, preferredSite string) string {
	preferredSite = strings.TrimSpace(preferredSite)
	if preferredSite != "" && !isDomesticLine(preferredSite) {
		for _, site := range sites {
			if site.Name == preferredSite || strings.Contains(site.Name, preferredSite) {
				return site.Name
			}
		}
		return preferredSite
	}
	for _, hint := range []string{"澳大利亚", "日本", "韩国", "泰国", "英国", "美国", "加拿大", "香港", "台湾"} {
		for _, site := range sites {
			if strings.Contains(site.Name, hint) {
				return site.Name
			}
		}
	}
	if preferredSite != "" {
		return preferredSite
	}
	if len(sites) > 0 {
		return sites[0].Name
	}
	return ""
}

func isDomesticLine(siteName string) bool {
	return strings.Contains(siteName, "国内专线")
}

func loginContactSectionScript(qrPath string) string {
	return `
$contactQrPath = '` + psSingleQuoted(qrPath) + `'
$contactPanel = [System.Windows.Forms.Panel]::new()
$contactPanel.Location = [System.Drawing.Point]::new(28, 500)
$contactPanel.Size = [System.Drawing.Size]::new(488, 238)
$contactPanel.BackColor = $panelAlt
$shell.Controls.Add($contactPanel)
Set-RoundedRegion $contactPanel 16

$contactTitle = [System.Windows.Forms.Label]::new()
$contactTitle.Location = [System.Drawing.Point]::new(0, 12)
$contactTitle.Size = [System.Drawing.Size]::new(488, 28)
$contactTitle.Text = '联系作者'
$contactTitle.TextAlign = [System.Drawing.ContentAlignment]::MiddleCenter
$contactTitle.ForeColor = $ink
$contactTitle.Font = New-UiFont 'Microsoft YaHei UI' 12 ([System.Drawing.FontStyle]::Bold)
$contactPanel.Controls.Add($contactTitle)

$contactHint = [System.Windows.Forms.Label]::new()
$contactHint.Location = [System.Drawing.Point]::new(0, 40)
$contactHint.Size = [System.Drawing.Size]::new(488, 22)
$contactHint.Text = '微信扫码添加作者'
$contactHint.TextAlign = [System.Drawing.ContentAlignment]::MiddleCenter
$contactHint.ForeColor = $muted
$contactPanel.Controls.Add($contactHint)

$contactQr = [System.Windows.Forms.PictureBox]::new()
$contactQr.Location = [System.Drawing.Point]::new(149, 62)
$contactQr.Size = [System.Drawing.Size]::new(190, 162)
$contactQr.BackColor = [System.Drawing.Color]::White
$contactQr.BorderStyle = [System.Windows.Forms.BorderStyle]::FixedSingle
$contactQr.SizeMode = [System.Windows.Forms.PictureBoxSizeMode]::Zoom
$contactPanel.Controls.Add($contactQr)

try {
    if (!(Test-Path -LiteralPath $contactQrPath)) {
        throw 'Contact QR code is missing.'
    }
    $contactQr.Image = [System.Drawing.Image]::FromFile($contactQrPath)
} catch {
    $contactQr.Visible = $false
    $contactError = [System.Windows.Forms.Label]::new()
    $contactError.Location = [System.Drawing.Point]::new(0, 118)
    $contactError.Size = [System.Drawing.Size]::new(488, 28)
    $contactError.Text = '二维码加载失败'
    $contactError.TextAlign = [System.Drawing.ContentAlignment]::MiddleCenter
    $contactError.ForeColor = [System.Drawing.Color]::FromArgb(174, 70, 55)
    $contactPanel.Controls.Add($contactError)
}

$form.Add_FormClosed({
    if ($contactQr.Image) {
        $contactQr.Image.Dispose()
        $contactQr.Image = $null
    }
})
`
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
	preferredSite = loginDefaultSite(sites, preferredSite)
	preferredSite = psSingleQuoted(preferredSite)
	savedUsername = psSingleQuoted(savedUsername)
	exePath, _ := os.Executable()
	iconPath := psSingleQuoted(filepath.Join(filepath.Dir(exePath), "app.ico"))
	backgroundPath := psSingleQuoted(filepath.Join(filepath.Dir(exePath), "ui-assets", "desktop-login-bg.png"))
	contactSectionScript := loginContactSectionScript(filepath.Join(filepath.Dir(exePath), "ui-assets", "wechat-contact-qr.png"))
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
$form.Size = New-Object System.Drawing.Size(620, 850)
$form.StartPosition = 'CenterScreen'
$form.FormBorderStyle = 'FixedDialog'
$form.MaximizeBox = $false
$form.MinimizeBox = $false
$form.ShowInTaskbar = $true
$form.TopMost = $true
$form.BackColor = [System.Drawing.Color]::FromArgb(250, 244, 220)
$form.Font = New-Object System.Drawing.Font('Microsoft YaHei UI', 9)
$form.AutoScaleMode = [System.Windows.Forms.AutoScaleMode]::Dpi
$iconPath = '` + iconPath + `'
$backgroundPath = '` + backgroundPath + `'
$resultPath = '` + escapedResultPath + `'
if (Test-Path $iconPath) {
    $form.Icon = New-Object System.Drawing.Icon($iconPath)
}
$script:hasBackgroundImage = $false
if (Test-Path $backgroundPath) {
    $form.BackgroundImage = [System.Drawing.Image]::FromFile($backgroundPath)
    $form.BackgroundImageLayout = [System.Windows.Forms.ImageLayout]::Stretch
    $script:hasBackgroundImage = $true
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

function Show-RechargeDialog($owner) {
    $dialog = [System.Windows.Forms.Form]::new()
    $dialog.Text = '账号充值说明'
    $dialog.Size = [System.Drawing.Size]::new(680, 650)
    $dialog.StartPosition = 'CenterParent'
    $dialog.FormBorderStyle = 'FixedDialog'
    $dialog.MaximizeBox = $false
    $dialog.MinimizeBox = $false
    $dialog.BackColor = [System.Drawing.Color]::FromArgb(255, 253, 242)
    $dialog.Font = [System.Drawing.Font]::new('Microsoft YaHei UI', 9)
    if (Test-Path $iconPath) {
        $dialog.Icon = [System.Drawing.Icon]::new($iconPath)
    }

    $title = [System.Windows.Forms.Label]::new()
    $title.Location = [System.Drawing.Point]::new(24, 18)
    $title.Size = [System.Drawing.Size]::new(610, 34)
    $title.Text = '线上购买与充值流程'
    $title.Font = [System.Drawing.Font]::new('Microsoft YaHei UI', 16, [System.Drawing.FontStyle]::Bold)
    $title.ForeColor = [System.Drawing.Color]::FromArgb(48, 84, 53)
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
    $box.Font = [System.Drawing.Font]::new('Microsoft YaHei UI', 10)
    $dialog.Controls.Add($box)

    $btnCopyLink = [System.Windows.Forms.Button]::new()
    $btnCopyLink.Location = [System.Drawing.Point]::new(24, 556)
    $btnCopyLink.Size = [System.Drawing.Size]::new(126, 38)
    $btnCopyLink.Text = '复制注册链接'
    $btnCopyLink.Add_Click({
        Set-Clipboard -Value 'https://vip90123.com/signup'
        [System.Windows.Forms.MessageBox]::Show($dialog, '注册链接已复制。', '已复制', 'OK', 'Information') | Out-Null
    })
    $dialog.Controls.Add($btnCopyLink)

    $btnCopyCode = [System.Windows.Forms.Button]::new()
    $btnCopyCode.Location = [System.Drawing.Point]::new(160, 556)
    $btnCopyCode.Size = [System.Drawing.Size]::new(126, 38)
    $btnCopyCode.Text = '复制推荐码'
    $btnCopyCode.Add_Click({
        Set-Clipboard -Value 'Sm3xWXkUif'
        [System.Windows.Forms.MessageBox]::Show($dialog, '推荐码已复制。', '已复制', 'OK', 'Information') | Out-Null
    })
    $dialog.Controls.Add($btnCopyCode)

    $btnClose = [System.Windows.Forms.Button]::new()
    $btnClose.Location = [System.Drawing.Point]::new(514, 556)
    $btnClose.Size = [System.Drawing.Size]::new(126, 38)
    $btnClose.Text = '知道了'
    $btnClose.DialogResult = [System.Windows.Forms.DialogResult]::OK
    $dialog.AcceptButton = $btnClose
    $dialog.Controls.Add($btnClose)

    [void]$dialog.ShowDialog($owner)
}

$accent = [System.Drawing.Color]::FromArgb(106, 174, 113)
$accentDark = [System.Drawing.Color]::FromArgb(74, 139, 91)
$amber = [System.Drawing.Color]::FromArgb(184, 132, 48)
$ink = [System.Drawing.Color]::FromArgb(48, 84, 53)
$muted = [System.Drawing.Color]::FromArgb(97, 116, 78)
$panel = [System.Drawing.Color]::FromArgb(255, 253, 242)
$panelAlt = [System.Drawing.Color]::FromArgb(241, 249, 230)

$form.Add_Paint({
    param($sender, $e)
    if ($script:hasBackgroundImage) { return }
    $rect = $sender.ClientRectangle
    if ($rect.Width -le 0 -or $rect.Height -le 0) { return }
    $brush = [System.Drawing.Drawing2D.LinearGradientBrush]::new(
        $rect,
        [System.Drawing.Color]::FromArgb(250, 244, 220),
        [System.Drawing.Color]::FromArgb(219, 239, 211),
        28
    )
    $e.Graphics.FillRectangle($brush, $rect)
    $brush.Dispose()

    $pen = New-Object System.Drawing.Pen([System.Drawing.Color]::FromArgb(217, 232, 196), 1)
    for ($x = 32; $x -lt 620; $x += 58) {
        $e.Graphics.DrawLine($pen, $x, 0, $x - 96, 590)
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
$shell.Location = New-Object System.Drawing.Point(28, 24)
$shell.Size = New-Object System.Drawing.Size(544, 770)
$shell.BackColor = [System.Drawing.Color]::FromArgb(255, 253, 242)
$form.Controls.Add($shell)
Set-RoundedRegion $shell 18

$topLine = New-Object System.Windows.Forms.Panel
$topLine.Location = New-Object System.Drawing.Point(0, 0)
$topLine.Size = New-Object System.Drawing.Size(544, 5)
$topLine.BackColor = $accent
$shell.Controls.Add($topLine)

$header = New-Object System.Windows.Forms.Panel
$header.Location = New-Object System.Drawing.Point(28, 28)
$header.Size = New-Object System.Drawing.Size(488, 86)
$header.BackColor = [System.Drawing.Color]::Transparent
$shell.Controls.Add($header)

$badge = New-Object System.Windows.Forms.Panel
$badge.Location = New-Object System.Drawing.Point(0, 0)
$badge.Size = New-Object System.Drawing.Size(70, 70)
$badge.BackColor = [System.Drawing.Color]::FromArgb(238, 248, 226)
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
$title.Size = New-Object System.Drawing.Size(260, 34)
$title.Text = '分流守卫'
$title.ForeColor = $ink
$title.Font = New-UiFont 'Microsoft YaHei UI' 20 ([System.Drawing.FontStyle]::Bold)
$header.Controls.Add($title)

$subtitle = New-Object System.Windows.Forms.Label
$subtitle.Location = New-Object System.Drawing.Point(88, 56)
$subtitle.Size = New-Object System.Drawing.Size(300, 22)
$subtitle.Text = '像手机端一样轻松开启云上小路'
$subtitle.ForeColor = $muted
$subtitle.Font = New-UiFont 'Microsoft YaHei UI' 9 ([System.Drawing.FontStyle]::Regular)
$header.Controls.Add($subtitle)

$pill = New-Object System.Windows.Forms.Panel
$pill.Location = New-Object System.Drawing.Point(386, 19)
$pill.Size = New-Object System.Drawing.Size(96, 28)
$pill.BackColor = [System.Drawing.Color]::FromArgb(230, 246, 221)
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
$content.Size = New-Object System.Drawing.Size(488, 226)
$content.BackColor = $panel
$shell.Controls.Add($content)
Set-RoundedRegion $content 14

$lblSite = New-Object System.Windows.Forms.Label
$lblSite.Location = New-Object System.Drawing.Point(22, 18)
$lblSite.Size = New-Object System.Drawing.Size(444, 20)
$lblSite.Text = 'VPN 节点'
$lblSite.ForeColor = $muted
$lblSite.Font = New-UiFont 'Microsoft YaHei UI' 9 ([System.Drawing.FontStyle]::Regular)
$content.Controls.Add($lblSite)

$cboSite = New-Object System.Windows.Forms.ComboBox
$cboSite.Location = New-Object System.Drawing.Point(22, 42)
$cboSite.Size = New-Object System.Drawing.Size(444, 30)
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
$txtUser.Size = New-Object System.Drawing.Size(210, 30)
$txtUser.Font = New-UiFont 'Bahnschrift' 11 ([System.Drawing.FontStyle]::Regular)
$txtUser.Text = $savedUsername
$txtUser.BorderStyle = 'FixedSingle'
$txtUser.BackColor = [System.Drawing.Color]::FromArgb(246, 249, 251)
$txtUser.ForeColor = [System.Drawing.Color]::FromArgb(15, 23, 42)
$content.Controls.Add($txtUser)

$lblPass = New-Object System.Windows.Forms.Label
$lblPass.Location = New-Object System.Drawing.Point(256, 92)
$lblPass.Size = New-Object System.Drawing.Size(180, 20)
$lblPass.Text = '密码'
$lblPass.ForeColor = $muted
$content.Controls.Add($lblPass)

$txtPass = New-Object System.Windows.Forms.TextBox
$txtPass.Location = New-Object System.Drawing.Point(256, 116)
$txtPass.Size = New-Object System.Drawing.Size(210, 30)
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
$chkRemember.Size = New-Object System.Drawing.Size(300, 24)
$chkRemember.Text = '记住并下次自动连接'
$chkRemember.ForeColor = $muted
$chkRemember.BackColor = $panel
$chkRemember.Checked = ` + rememberChecked + `
$content.Controls.Add($chkRemember)

$bottomBar = New-Object System.Windows.Forms.Panel
$bottomBar.Location = New-Object System.Drawing.Point(28, 376)
$bottomBar.Size = New-Object System.Drawing.Size(488, 1)
$bottomBar.BackColor = [System.Drawing.Color]::FromArgb(214, 229, 190)
$shell.Controls.Add($bottomBar)

$hint = New-Object System.Windows.Forms.Label
$hint.Location = New-Object System.Drawing.Point(30, 394)
$hint.Size = New-Object System.Drawing.Size(220, 26)
$hint.Text = '首次使用请先确认账号套餐已开通'
$hint.ForeColor = $muted
$hint.Font = New-UiFont 'Microsoft YaHei UI' 8 ([System.Drawing.FontStyle]::Regular)
$shell.Controls.Add($hint)

$btnOK = New-Object System.Windows.Forms.Button
$btnOK.Location = New-Object System.Drawing.Point(368, 390)
$btnOK.Size = New-Object System.Drawing.Size(148, 44)
$btnOK.Text = '连接 VPN'
$btnOK.FlatStyle = 'Flat'
$btnOK.BackColor = $accentDark
$btnOK.ForeColor = [System.Drawing.Color]::White
$btnOK.Font = New-UiFont 'Microsoft YaHei UI' 10 ([System.Drawing.FontStyle]::Bold)
$btnOK.FlatAppearance.BorderSize = 0
$btnOK.Cursor = [System.Windows.Forms.Cursors]::Hand
$btnOK.DialogResult = [System.Windows.Forms.DialogResult]::OK
$btnOK.Add_MouseEnter({ $btnOK.BackColor = [System.Drawing.Color]::FromArgb(92, 164, 99) })
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
$btnCancel.Location = New-Object System.Drawing.Point(256, 390)
$btnCancel.Size = New-Object System.Drawing.Size(96, 44)
$btnCancel.Text = '取消'
$btnCancel.FlatStyle = 'Flat'
$btnCancel.BackColor = [System.Drawing.Color]::FromArgb(246, 238, 209)
$btnCancel.ForeColor = [System.Drawing.Color]::FromArgb(77, 97, 54)
$btnCancel.FlatAppearance.BorderSize = 0
$btnCancel.Cursor = [System.Windows.Forms.Cursors]::Hand
$btnCancel.DialogResult = [System.Windows.Forms.DialogResult]::Cancel
$btnCancel.Add_MouseEnter({ $btnCancel.BackColor = [System.Drawing.Color]::FromArgb(238, 226, 190) })
$btnCancel.Add_MouseLeave({ $btnCancel.BackColor = [System.Drawing.Color]::FromArgb(246, 238, 209) })
$form.CancelButton = $btnCancel
$shell.Controls.Add($btnCancel)
Set-RoundedRegion $btnCancel 12

$btnRechargeHelp = New-Object System.Windows.Forms.Button
$btnRechargeHelp.Location = New-Object System.Drawing.Point(394, 450)
$btnRechargeHelp.Size = New-Object System.Drawing.Size(122, 34)
$btnRechargeHelp.Text = '充值说明'
$btnRechargeHelp.FlatStyle = 'Flat'
$btnRechargeHelp.BackColor = [System.Drawing.Color]::FromArgb(238, 248, 226)
$btnRechargeHelp.ForeColor = [System.Drawing.Color]::FromArgb(48, 84, 53)
$btnRechargeHelp.Font = New-UiFont 'Microsoft YaHei UI' 9 ([System.Drawing.FontStyle]::Bold)
$btnRechargeHelp.FlatAppearance.BorderSize = 0
$btnRechargeHelp.Cursor = [System.Windows.Forms.Cursors]::Hand
$btnRechargeHelp.Add_MouseEnter({ $btnRechargeHelp.BackColor = [System.Drawing.Color]::FromArgb(224, 242, 210) })
$btnRechargeHelp.Add_MouseLeave({ $btnRechargeHelp.BackColor = [System.Drawing.Color]::FromArgb(238, 248, 226) })
$btnRechargeHelp.Add_Click({ Show-RechargeDialog $form })
$shell.Controls.Add($btnRechargeHelp)
Set-RoundedRegion $btnRechargeHelp 12
` + contactSectionScript + `
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
