//go:build windows

package main

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/user/anyconnect-split/internal/config"
	"github.com/user/anyconnect-split/internal/installation"
	"github.com/user/anyconnect-split/internal/ipdb"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	installFolder       = "AnyConnectSplitTunnel"
	installRegistryPath = `Software\AnyConnectSplitTunnel`
	appExeName          = "anyconnect-split.exe"
	hideWindowFlag      = 0x08000000
)

const (
	shortcutName         = "分流守卫.lnk"
	legacyShortcutName   = "Split Tunnel.lnk"
	shortcutIconFileName = "app-shortcut.ico"
	shortcutDescription  = "分流守卫"
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
	case "--prepare-overwrite":
		err = requireArg(args, 2, "missing install directory")
		if err == nil {
			err = prepareOverwriteInstall(args[1])
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
	case "--has-bundled-tun-tools":
		err = requireArg(args, 2, "missing install directory")
		if err == nil && hasBundledTunTools(args[1]) {
			os.Exit(0)
		}
		os.Exit(2)
	case "--create-shortcuts":
		err = requireArg(args, 2, "missing install directory")
		if err == nil {
			installDir := args[1]
			err = createShortcuts(filepath.Join(installDir, appExeName), installDir)
		}
	case "--start-app":
		err = requireArg(args, 2, "missing install directory")
		if err == nil {
			installDir := args[1]
			err = startApp(filepath.Join(installDir, appExeName), installDir)
		}
	case "--write-install-state":
		err = requireArg(args, 2, "missing install directory")
		if err == nil {
			err = writeInstallState(args[1])
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
	if err := requireNativeUIRuntime(); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// The compiled host is extracted, never a script or credential-bearing file.
	data, err := payload.ReadFile("payload/anyconnect-ui.exe")
	if err != nil {
		return fmt.Errorf("native UI host missing: %w", err)
	}
	dir, err := os.MkdirTemp("", "anyconnect-setup-ui-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	host := filepath.Join(dir, "anyconnect-ui.exe")
	if err := os.WriteFile(host, data, 0600); err != nil {
		return err
	}
	request, err := json.Marshal(map[string]any{"parent_pid": os.Getpid(), "installer_path": exe, "default_install_dir": defaultInstallDir(), "asset_root": dir})
	if err != nil {
		return err
	}
	cmd := exec.Command(host, "installer")
	cmd.Stdin = bytes.NewReader(request)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: hideWindowFlag}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("native installer UI: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func requireNativeUIRuntime() error {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\NET Framework Setup\NDP\v4\Full`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return fmt.Errorf("原生界面需要 Microsoft .NET Framework 4.8 或更高版本，请从 Microsoft 官方来源安装后重试")
	}
	defer key.Close()
	release, _, err := key.GetIntegerValue("Release")
	if err != nil || release < 528040 {
		return fmt.Errorf("原生界面需要 Microsoft .NET Framework 4.8 或更高版本，请更新后重试")
	}
	return nil
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
	if dir := installedDirFromRegistry(); dir != "" {
		return dir
	}
	base := os.Getenv("ProgramFiles")
	if base == "" {
		base = os.Getenv("ProgramFiles(x86)")
	}
	if base == "" {
		base = filepath.Join(os.Getenv("SystemDrive")+`\`, "Program Files")
	}
	return filepath.Join(base, installFolder)
}

func installedDirFromRegistry() string {
	for _, access := range []uint32{
		registry.QUERY_VALUE | registry.WOW64_64KEY,
		registry.QUERY_VALUE,
	} {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, installRegistryPath, access)
		if err != nil {
			continue
		}
		value, _, err := key.GetStringValue("InstallDir")
		_ = key.Close()
		if err != nil {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		return filepath.Clean(value)
	}
	return ""
}

var releaseVersion = "1.0.4.0"
var releasePublisherBase64 string

func writeInstallState(installDir string) error {
	if _, err := os.Stat(filepath.Join(installDir, installation.Uninstaller)); err != nil {
		return fmt.Errorf("uninstaller is missing: %w", err)
	}
	key, _, err := registry.CreateKey(
		registry.LOCAL_MACHINE,
		installRegistryPath,
		registry.SET_VALUE|registry.WOW64_64KEY,
	)
	if err != nil {
		return err
	}
	defer key.Close()

	installDir = filepath.Clean(installDir)
	if err := key.SetStringValue("InstallDir", installDir); err != nil {
		return err
	}
	if err := key.SetStringValue("DisplayName", shortcutDescription); err != nil {
		return err
	}
	if err := key.SetStringValue("ExecutablePath", filepath.Join(installDir, appExeName)); err != nil {
		return err
	}
	if err := key.SetStringValue("ShortcutName", shortcutName); err != nil {
		return err
	}
	publisher, err := base64.StdEncoding.DecodeString(releasePublisherBase64)
	if err != nil {
		return fmt.Errorf("invalid publisher metadata: %w", err)
	}
	return installation.Register(installDir, releaseVersion, string(publisher))
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

func prepareOverwriteInstall(installDir string) error {
	appPath := filepath.Clean(filepath.Join(installDir, appExeName))
	script := prepareOverwriteScript(appPath)

	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: hideWindowFlag}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func prepareOverwriteScript(appPath string) string {
	return fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$target = [System.IO.Path]::GetFullPath(%s)
$installDir = [System.IO.Path]::GetDirectoryName($target)
$dashboardStatePath = Join-Path $installDir 'data\dashboard-state.json'

function Get-TargetAppProcesses {
    return @(Get-CimInstance Win32_Process -Filter "Name = 'anyconnect-split.exe'" -ErrorAction SilentlyContinue |
        Where-Object {
            $_.ExecutablePath -and
            ([System.IO.Path]::GetFullPath($_.ExecutablePath) -ieq $target)
        })
}

function Get-TargetDashboardProcesses {
    return @(Get-CimInstance Win32_Process -Filter "Name = 'powershell.exe'" -ErrorAction SilentlyContinue |
        Where-Object {
            $commandLine = [string]$_.CommandLine
            (-not [string]::IsNullOrWhiteSpace($commandLine)) -and
            ($commandLine.IndexOf($dashboardStatePath, [System.StringComparison]::OrdinalIgnoreCase) -ge 0) -and
            ($commandLine.IndexOf('AnyConnect 分流管理台', [System.StringComparison]::OrdinalIgnoreCase) -ge 0)
        })
}

$deadline = (Get-Date).AddSeconds(8)
do {
    $matches = Get-TargetAppProcesses
    foreach ($p in $matches) {
        Start-Process -FilePath 'taskkill.exe' -ArgumentList @('/PID', [string]$p.ProcessId, '/T', '/F') -Wait -WindowStyle Hidden | Out-Null
    }

    $dashboards = Get-TargetDashboardProcesses
    foreach ($p in $dashboards) {
        Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue
    }

    if ($matches.Count -eq 0 -and $dashboards.Count -eq 0) { break }
    Start-Sleep -Milliseconds 250
} while ((Get-Date) -lt $deadline)

$stillRunning = Get-TargetAppProcesses
$stillDashboards = Get-TargetDashboardProcesses
if ($stillRunning.Count -gt 0 -or $stillDashboards.Count -gt 0) {
    throw '旧版本仍在运行，无法覆盖安装。请先退出分流守卫后重试。'
}
`, psQuote(appPath))
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

func hasBundledTunTools(installDir string) bool {
	for _, rel := range []string{
		filepath.Join("openconnect", "openconnect.exe"),
		filepath.Join("tools", "sing-box.exe"),
	} {
		info, err := os.Stat(filepath.Join(installDir, rel))
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
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
	iconPath := shortcutIconPath(installDir)
	for _, shortcut := range shortcutTargets(os.Getenv("PUBLIC"), os.Getenv("ProgramData")) {
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
	cleanupLegacyShortcuts(os.Getenv("PUBLIC"), os.Getenv("ProgramData"))
	return nil
}

func shortcutTargets(publicDir, programData string) []string {
	return []string{
		filepath.Join(publicDir, "Desktop", shortcutName),
		filepath.Join(programData, "Microsoft", "Windows", "Start Menu", "Programs", shortcutName),
	}
}

func legacyShortcutTargets(publicDir, programData string) []string {
	return []string{
		filepath.Join(publicDir, "Desktop", legacyShortcutName),
		filepath.Join(programData, "Microsoft", "Windows", "Start Menu", "Programs", legacyShortcutName),
	}
}

func shortcutIconPath(installDir string) string {
	return filepath.Join(installDir, shortcutIconFileName)
}

func cleanupLegacyShortcuts(publicDir, programData string) {
	for _, shortcut := range legacyShortcutTargets(publicDir, programData) {
		if shortcut == "" {
			continue
		}
		_ = os.Remove(shortcut)
	}
}

func createShortcut(shortcut, target, workDir, icon string) error {
	script := fmt.Sprintf(
		`$ws = New-Object -ComObject WScript.Shell; $sc = $ws.CreateShortcut(%s); $sc.TargetPath = %s; $sc.WorkingDirectory = %s; $sc.IconLocation = %s; $sc.Description = %s; $sc.Save()`,
		psQuote(shortcut),
		psQuote(target),
		psQuote(workDir),
		psQuote(icon+",0"),
		psQuote(shortcutDescription),
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
