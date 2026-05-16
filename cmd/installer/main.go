//go:build windows

package main

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/user/anyconnect-split/internal/config"
	"github.com/user/anyconnect-split/internal/ipdb"
	"golang.org/x/sys/windows"
)

const (
	installFolder  = "AnyConnectSplitTunnel"
	hideWindowFlag = 0x08000000
)

//go:embed payload/**
var payload embed.FS

func main() {
	if handled := handleCommand(os.Args[1:]); handled {
		return
	}
	if !isAdmin() {
		relaunchElevated()
		return
	}
	if err := runWizard(); err != nil {
		showError("安装失败", err.Error())
		os.Exit(1)
	}
}

func handleCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if !isAdmin() && args[0] != "--has-cisco" {
		os.Exit(5)
	}

	var err error
	switch args[0] {
	case "--install-payload":
		err = requireArg(args, 2, "missing install directory")
		if err == nil {
			err = installPayload(args[1])
		}
	case "--extract-cisco":
		err = requireArg(args, 2, "missing output directory")
		if err == nil {
			err = extractBundledCisco(args[1])
		}
	case "--has-cisco":
		if hasCiscoClient() {
			os.Exit(0)
		}
		os.Exit(2)
	case "--create-shortcuts":
		err = requireArg(args, 2, "missing install directory")
		if err == nil {
			installDir := args[1]
			err = createShortcuts(filepath.Join(installDir, "anyconnect-split.exe"), installDir)
		}
	case "--start-app":
		err = requireArg(args, 2, "missing install directory")
		if err == nil {
			installDir := args[1]
			err = startApp(filepath.Join(installDir, "anyconnect-split.exe"), installDir)
		}
	default:
		return false
	}
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
	return true
}

func requireArg(args []string, count int, message string) error {
	if len(args) < count {
		return errors.New(message)
	}
	return nil
}

func runWizard() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("anyconnect-install-%d.ps1", os.Getpid()))
	if err := os.WriteFile(scriptPath, powerShellScriptBytes(wizardScript), 0600); err != nil {
		return err
	}
	defer os.Remove(scriptPath)

	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-STA",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
		"-InstallerPath", exe,
		"-DefaultInstallDir", defaultInstallDir(),
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: hideWindowFlag}
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text != "" {
			return fmt.Errorf("%w: %s", err, text)
		}
		return err
	}
	return nil
}

func powerShellScriptBytes(script string) []byte {
	encoded := utf16.Encode([]rune(script))
	data := make([]byte, 2+len(encoded)*2)
	data[0] = 0xff
	data[1] = 0xfe
	for i, r := range encoded {
		data[2+i*2] = byte(r)
		data[3+i*2] = byte(r >> 8)
	}
	return data
}

func isAdmin() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)
	token := windows.Token(0)
	member, err := token.IsMember(sid)
	return err == nil && member
}

func relaunchElevated() {
	exe, err := os.Executable()
	if err != nil {
		showError("安装失败", "无法获取安装器路径："+err.Error())
		return
	}
	verbPtr, _ := windows.UTF16PtrFromString("runas")
	exePtr, _ := windows.UTF16PtrFromString(exe)
	cwdPtr, _ := windows.UTF16PtrFromString(filepath.Dir(exe))
	if err := windows.ShellExecute(0, verbPtr, exePtr, nil, cwdPtr, windows.SW_NORMAL); err != nil {
		showError("需要管理员权限", "请以管理员身份运行安装器："+err.Error())
	}
}

func defaultInstallDir() string {
	base := os.Getenv("ProgramFiles")
	if base == "" {
		base = os.Getenv("ProgramFiles(x86)")
	}
	if base == "" {
		base = filepath.Join(os.Getenv("SystemDrive")+`\`, "Program Files")
	}
	return filepath.Join(base, installFolder)
}

func installPayload(installDir string) error {
	return fs.WalkDir(payload, "payload", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel("payload", path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if shouldSkipPayload(rel) {
			return nil
		}

		data, err := payload.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(installDir, filepath.FromSlash(rel))
		if shouldPreserveExisting(rel, dst) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0644)
	})
}

func shouldSkipPayload(rel string) bool {
	name := filepath.Base(rel)
	if name == "README.txt" || name == ".gitignore" {
		return true
	}
	return strings.HasPrefix(rel, "cisco/")
}

func shouldPreserveExisting(rel, dst string) bool {
	switch rel {
	case "configs/config.yaml":
		_, err := os.Stat(dst)
		return err == nil
	case "data/china_ip_list.txt":
		return ipdb.FileHasCIDRs(dst)
	default:
		return false
	}
}

func hasCiscoClient() bool {
	return config.DetectVPNCLIPath() != ""
}

func bundledCiscoInstallers() ([]string, error) {
	var installers []string
	err := fs.WalkDir(payload, "payload/cisco", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".msi" || ext == ".exe" {
			installers = append(installers, path)
		}
		return nil
	})
	return installers, err
}

func extractBundledCisco(outDir string) error {
	installers, err := bundledCiscoInstallers()
	if err != nil {
		return err
	}
	if len(installers) == 0 {
		return fmt.Errorf("installer payload has no Cisco package")
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	for _, src := range installers {
		data, err := payload.ReadFile(src)
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, filepath.Base(src))
		if err := os.WriteFile(dst, data, 0644); err != nil {
			return err
		}
	}
	return nil
}

func createShortcuts(appPath, installDir string) error {
	iconPath := filepath.Join(installDir, "app.ico")
	shortcutTargets := []string{
		filepath.Join(os.Getenv("PUBLIC"), "Desktop", "Split Tunnel.lnk"),
		filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Start Menu", "Programs", "Split Tunnel.lnk"),
	}
	for _, shortcut := range shortcutTargets {
		if shortcut == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(shortcut), 0755); err != nil {
			return err
		}
		if err := createShortcut(shortcut, appPath, installDir, iconPath); err != nil {
			return err
		}
	}
	return nil
}

func createShortcut(shortcut, target, workDir, icon string) error {
	script := fmt.Sprintf(
		`$ws = New-Object -ComObject WScript.Shell; $sc = $ws.CreateShortcut(%s); $sc.TargetPath = %s; $sc.WorkingDirectory = %s; $sc.IconLocation = %s; $sc.Description = 'AnyConnect Split Tunnel'; $sc.Save()`,
		psQuote(shortcut),
		psQuote(target),
		psQuote(workDir),
		psQuote(icon),
	)
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: hideWindowFlag}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func startApp(appPath, installDir string) error {
	cmd := exec.Command(appPath)
	cmd.Dir = installDir
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: hideWindowFlag}
	return cmd.Start()
}

func showError(title, message string) {
	messageBox(title, message, 0x00000010)
}

func messageBox(title, message string, icon uintptr) {
	user32 := syscall.NewLazyDLL("user32.dll")
	messageBoxW := user32.NewProc("MessageBoxW")
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	msgPtr, _ := syscall.UTF16PtrFromString(message)
	messageBoxW.Call(0, uintptr(unsafe.Pointer(msgPtr)), uintptr(unsafe.Pointer(titlePtr)), 0x00040000|icon)
}

const wizardScript = `
param(
    [Parameter(Mandatory=$true)][string]$InstallerPath,
    [Parameter(Mandatory=$true)][string]$DefaultInstallDir
)

$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()

function Invoke-InstallerCommand {
    param(
        [Parameter(Mandatory=$true)][string[]]$Arguments,
        [Parameter(Mandatory=$true)][string]$FailureMessage
    )
    $argLine = ($Arguments | ForEach-Object { '"' + ($_.Replace('"', '\"')) + '"' }) -join ' '
    $p = Start-Process -FilePath $InstallerPath -ArgumentList $argLine -Wait -PassThru -WindowStyle Hidden
    if ($p.ExitCode -ne 0) {
        throw $FailureMessage
    }
}

function Test-CiscoInstalled {
    $p = Start-Process -FilePath $InstallerPath -ArgumentList '--has-cisco' -Wait -PassThru -WindowStyle Hidden
    return $p.ExitCode -eq 0
}

function Normalize-InstallPath($path) {
    $full = [System.IO.Path]::GetFullPath($path)
    $root = [System.IO.Path]::GetPathRoot($full)
    if ($full.TrimEnd([char]92) -eq $root.TrimEnd([char]92)) {
        return (Join-Path $full 'AnyConnectSplitTunnel')
    }
    return $full.TrimEnd([char]92)
}

function New-UiFont($size, $style) {
    return [System.Drawing.Font]::new('Microsoft YaHei UI', [single]$size, $style)
}

$form = New-Object System.Windows.Forms.Form
$form.Text = 'AnyConnect Split Tunnel 安装'
$form.Size = New-Object System.Drawing.Size(620, 390)
$form.StartPosition = 'CenterScreen'
$form.FormBorderStyle = 'FixedDialog'
$form.MaximizeBox = $false
$form.MinimizeBox = $false
$form.TopMost = $true
$form.BackColor = [System.Drawing.Color]::FromArgb(248, 250, 252)
$form.Font = New-UiFont 9 ([System.Drawing.FontStyle]::Regular)

$title = New-Object System.Windows.Forms.Label
$title.Location = New-Object System.Drawing.Point(26, 24)
$title.Size = New-Object System.Drawing.Size(540, 34)
$title.Text = '安装 AnyConnect Split Tunnel'
$title.ForeColor = [System.Drawing.Color]::FromArgb(15, 23, 42)
$title.Font = New-UiFont 16 ([System.Drawing.FontStyle]::Bold)
$form.Controls.Add($title)

$subtitle = New-Object System.Windows.Forms.Label
$subtitle.Location = New-Object System.Drawing.Point(28, 62)
$subtitle.Size = New-Object System.Drawing.Size(540, 42)
$subtitle.Text = '选择安装位置。若本机没有 Cisco 客户端，安装过程中会打开 Cisco 官方安装窗口，请按提示完成。'
$subtitle.ForeColor = [System.Drawing.Color]::FromArgb(71, 85, 105)
$form.Controls.Add($subtitle)

$pathLabel = New-Object System.Windows.Forms.Label
$pathLabel.Location = New-Object System.Drawing.Point(30, 120)
$pathLabel.Size = New-Object System.Drawing.Size(160, 22)
$pathLabel.Text = '安装位置'
$pathLabel.ForeColor = [System.Drawing.Color]::FromArgb(51, 65, 85)
$form.Controls.Add($pathLabel)

$txtPath = New-Object System.Windows.Forms.TextBox
$txtPath.Location = New-Object System.Drawing.Point(30, 146)
$txtPath.Size = New-Object System.Drawing.Size(430, 28)
$txtPath.Text = $DefaultInstallDir
$txtPath.Font = New-UiFont 10 ([System.Drawing.FontStyle]::Regular)
$form.Controls.Add($txtPath)

$btnBrowse = New-Object System.Windows.Forms.Button
$btnBrowse.Location = New-Object System.Drawing.Point(474, 144)
$btnBrowse.Size = New-Object System.Drawing.Size(104, 32)
$btnBrowse.Text = '浏览...'
$btnBrowse.UseVisualStyleBackColor = $true
$form.Controls.Add($btnBrowse)

$progress = New-Object System.Windows.Forms.ProgressBar
$progress.Location = New-Object System.Drawing.Point(30, 214)
$progress.Size = New-Object System.Drawing.Size(548, 24)
$progress.Minimum = 0
$progress.Maximum = 100
$progress.Value = 0
$form.Controls.Add($progress)

$status = New-Object System.Windows.Forms.Label
$status.Location = New-Object System.Drawing.Point(30, 250)
$status.Size = New-Object System.Drawing.Size(548, 44)
$status.Text = '准备安装'
$status.ForeColor = [System.Drawing.Color]::FromArgb(51, 65, 85)
$form.Controls.Add($status)

$btnInstall = New-Object System.Windows.Forms.Button
$btnInstall.Location = New-Object System.Drawing.Point(366, 306)
$btnInstall.Size = New-Object System.Drawing.Size(102, 34)
$btnInstall.Text = '开始安装'
$btnInstall.UseVisualStyleBackColor = $true
$form.Controls.Add($btnInstall)

$btnClose = New-Object System.Windows.Forms.Button
$btnClose.Location = New-Object System.Drawing.Point(476, 306)
$btnClose.Size = New-Object System.Drawing.Size(102, 34)
$btnClose.Text = '取消'
$btnClose.UseVisualStyleBackColor = $true
$form.Controls.Add($btnClose)

$btnBrowse.Add_Click({
    $dialog = New-Object System.Windows.Forms.FolderBrowserDialog
    $dialog.Description = '选择安装位置'
    $dialog.SelectedPath = $txtPath.Text
    $dialog.ShowNewFolderButton = $true
    if ($dialog.ShowDialog($form) -eq [System.Windows.Forms.DialogResult]::OK) {
        $txtPath.Text = $dialog.SelectedPath
    }
})

$script:installing = $false

function Set-InstallProgress($percent, $message) {
    if ($percent -ge 0) {
        $progress.Style = [System.Windows.Forms.ProgressBarStyle]::Continuous
        $progress.Value = [Math]::Min(100, [Math]::Max(0, $percent))
    } else {
        $progress.Style = [System.Windows.Forms.ProgressBarStyle]::Marquee
        $progress.MarqueeAnimationSpeed = 28
    }
    $status.Text = [string]$message
    [System.Windows.Forms.Application]::DoEvents()
}

function Set-InstallControlsEnabled($enabled) {
    $btnInstall.Enabled = $enabled
    $btnBrowse.Enabled = $enabled
    $txtPath.Enabled = $enabled
}

function Invoke-InstallSteps($installDir) {
    Set-InstallProgress 8 '正在准备安装目录...'
    Invoke-InstallerCommand -Arguments @('--install-payload', $installDir) -FailureMessage '安装主程序失败。'

    Set-InstallProgress 35 '主程序安装完成，正在检查 Cisco 客户端...'
    $hasCisco = Test-CiscoInstalled
    if (-not $hasCisco) {
        Set-InstallProgress 45 '正在释放 Cisco 官方安装包...'
        $tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ('anyconnect-cisco-' + [guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
        Invoke-InstallerCommand -Arguments @('--extract-cisco', $tempDir) -FailureMessage '安装包内没有包含 Cisco 官方安装文件。'

        $ciscoInstaller = Get-ChildItem -LiteralPath $tempDir -File |
            Where-Object { $_.Extension -in @('.msi', '.exe') } |
            Select-Object -First 1
        if ($null -eq $ciscoInstaller) {
            throw '未找到可运行的 Cisco 安装文件。'
        }

        Set-InstallProgress -1 '请在弹出的 Cisco 安装窗口中完成安装，完成后本安装器会继续。'
        if ($ciscoInstaller.Extension -ieq '.msi') {
            $p = Start-Process -FilePath 'msiexec.exe' -ArgumentList @('/i', ('"{0}"' -f $ciscoInstaller.FullName), '/norestart') -Wait -PassThru
        } else {
            $p = Start-Process -FilePath $ciscoInstaller.FullName -Wait -PassThru
        }
        if ($p.ExitCode -notin @(0, 3010)) {
            throw ('Cisco 安装未完成，退出码：' + $p.ExitCode)
        }
        Start-Sleep -Seconds 2
        if (-not (Test-CiscoInstalled)) {
            throw 'Cisco 安装结束后仍未检测到 vpncli.exe。若 Cisco 提示需要重启，请重启后从桌面快捷方式启动。'
        }
    }

    Set-InstallProgress 82 '正在创建桌面和开始菜单快捷方式...'
    Invoke-InstallerCommand -Arguments @('--create-shortcuts', $installDir) -FailureMessage '创建快捷方式失败。'
    Set-InstallProgress 94 '正在启动程序...'
    Invoke-InstallerCommand -Arguments @('--start-app', $installDir) -FailureMessage '启动程序失败。'
    Set-InstallProgress 100 '安装完成，登录窗口稍后会打开。请使用你自己的 VPN 账号密码登录。'
}

$btnInstall.Add_Click({
    if ($script:installing) {
        return
    }
    $path = $txtPath.Text.Trim()
    if ($path -eq '') {
        [System.Windows.Forms.MessageBox]::Show($form, '请选择安装位置。', '需要安装位置', 'OK', 'Warning') | Out-Null
        return
    }
    try {
        $path = Normalize-InstallPath $path
    } catch {
        [System.Windows.Forms.MessageBox]::Show($form, '安装位置无效。', '需要安装位置', 'OK', 'Warning') | Out-Null
        return
    }
    $txtPath.Text = $path
    $script:installing = $true
    Set-InstallControlsEnabled $false
    $btnClose.Text = '关闭'
    try {
        Invoke-InstallSteps $path
        [System.Windows.Forms.MessageBox]::Show($form, '安装完成。首次连接请在登录窗口输入自己的账号密码。', '安装完成', 'OK', 'Information') | Out-Null
    } catch {
        $progress.Style = [System.Windows.Forms.ProgressBarStyle]::Continuous
        $message = $_.Exception.Message
        $status.Text = '安装失败：' + $message
        [System.Windows.Forms.MessageBox]::Show($form, $message, '安装失败', 'OK', 'Error') | Out-Null
    } finally {
        $script:installing = $false
        Set-InstallControlsEnabled $true
        $btnClose.Text = '完成'
    }
})

$btnClose.Add_Click({
    if ($script:installing) {
        [System.Windows.Forms.MessageBox]::Show($form, '安装正在进行，请等待当前步骤完成。', '正在安装', 'OK', 'Information') | Out-Null
        return
    }
    $form.Close()
})

$form.Add_Shown({
    $form.Activate()
    $form.BringToFront()
})

[void]$form.ShowDialog()
`
