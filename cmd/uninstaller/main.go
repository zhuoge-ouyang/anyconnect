//go:build windows

package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"github.com/user/anyconnect-split/internal/installation"
	"golang.org/x/sys/windows"
)

// Filled by package.ps1 from the actual release payload, not a writable disk manifest.
var ownedFilesBase64 string

func main() {
	exe, err := os.Executable()
	if err != nil {
		fail(err)
	}
	root := filepath.Dir(exe)
	if err := validateRoot(root); err != nil {
		fail(err)
	}
	if err := installation.Verify(root); err != nil {
		fail(err)
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		verb, _ := windows.UTF16PtrFromString("runas")
		file, _ := windows.UTF16PtrFromString(exe)
		dir, _ := windows.UTF16PtrFromString(root)
		if err := windows.ShellExecute(0, verb, file, nil, dir, windows.SW_SHOWNORMAL); err != nil {
			fail(err)
		}
		return
	}
	if message("卸载分流守卫", "将卸载以下位置的分流守卫：\n"+root+"\n\n请先在托盘菜单中退出程序。\n不会卸载独立安装的 Cisco / OpenConnect，也不会删除其他文件。\n卸载程序自身会在下次重启时清理，不会自动重启电脑。\n\n是否继续？", 0x24|0x100) != 6 {
		return
	}
	choice := message("保留配置与日志", "是否保留配置、白名单和运行日志，方便下次安装？\n\n是：保留（默认）\n否：删除本程序已知的配置和运行文件\n取消：不卸载\n\nWindows 凭据管理器中的已保存密码始终保留，\n因为它们可能被其他安装副本共用；可在凭据管理器中单独删除。", 0x23)
	if choice == 2 {
		return
	}
	files, err := ownedFiles()
	if err != nil {
		fail(err)
	}
	if err := uninstall(root, files, choice == 6, systemHooks()); err != nil {
		fail(err)
	}
	text := "分流守卫已卸载。卸载程序自身将在下次重启后清理。\n未识别的文件和 Windows 已保存凭据没有删除。"
	if choice == 6 {
		text += "\n配置与日志保留在：\n" + root
	}
	message("卸载完成", text, 0x40)
}

func fail(err error) {
	message("卸载未完成", err.Error()+"\n\n请处理问题后重试；若安装文件不完整，可用完整安装包修复。", 0x10)
	os.Exit(1)
}

func message(title, text string, flags uintptr) uintptr {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	r, _, _ := windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags|0x40000)
	return r
}

func ownedFiles() ([]string, error) {
	b, err := base64.StdEncoding.DecodeString(ownedFilesBase64)
	if err != nil {
		return nil, err
	}
	var files []string
	if err := json.Unmarshal(b, &files); err != nil {
		return nil, fmt.Errorf("卸载文件清单无效：%w", err)
	}
	if len(files) == 0 {
		return nil, errors.New("卸载文件清单为空")
	}
	return files, nil
}

// No recursive deletion: unknown files, independent clients and shared drivers remain.
var runtimeFiles = []string{
	"configs/config.yaml", "split-tunnel.log",
	"data/dashboard-state.json", "data/dashboard-state.json.tmp",
	"data/applied_routes.json", "data/applied_vpn_routes.json",
	"data/openconnect-lite.js", "data/openconnect-state.txt", "data/sing-box-tun.json",
	"data/smart-select-transaction.json", "data/smart-select-history.json",
}

type hooks struct {
	preflight    func(string) error
	integrations func(string) error
	unregister   func(string) error
	scheduleSelf func(string) (func() error, error)
}

func uninstall(root string, files []string, preserve bool, h hooks) error {
	if err := validateRoot(root); err != nil {
		return err
	}
	paths, err := removalPaths(root, files, preserve)
	if err != nil {
		return err
	}
	if err := checkRoutes(root); err != nil {
		return err
	}
	if err := h.preflight(root); err != nil {
		return err
	}
	if err := h.integrations(root); err != nil {
		return err
	}
	for _, path := range paths {
		// Check again immediately before each operation; never follow reparse points.
		if err := noReparse(path); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("无法删除 %s：%w", path, err)
		}
	}
	// Empty owned directories only; os.Remove never recursively erases user files.
	dirs := map[string]bool{}
	for _, path := range paths {
		for d := filepath.Dir(path); !strings.EqualFold(d, root); d = filepath.Dir(d) {
			dirs[d] = true
		}
	}
	var ordered []string
	for d := range dirs {
		ordered = append(ordered, d)
	}
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	for _, d := range ordered {
		if err := noReparse(d); err != nil {
			return err
		}
		err := os.Remove(d)
		if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, windows.ERROR_DIR_NOT_EMPTY) {
			return err
		}
	}
	rollback, err := h.scheduleSelf(filepath.Join(root, installation.Uninstaller))
	if err != nil {
		return err
	}
	if err := h.unregister(root); err != nil {
		return errors.Join(err, rollback())
	}
	return nil
}

func removalPaths(root string, files []string, preserve bool) ([]string, error) {
	all := append(append([]string{}, files...), runtimeFiles...)
	seen := map[string]bool{}
	var paths []string
	for _, rel := range all {
		rel = filepath.FromSlash(rel)
		if !filepath.IsLocal(rel) || strings.Contains(rel, ":") || strings.ContainsAny(rel, "*?\x00") || filepath.Clean(rel) != rel {
			return nil, fmt.Errorf("不安全的卸载路径：%q", rel)
		}
		if strings.EqualFold(rel, installation.Uninstaller) {
			continue
		}
		lower := strings.ToLower(filepath.ToSlash(rel))
		if preserve && (strings.HasPrefix(lower, "configs/") || strings.HasPrefix(lower, "data/") || strings.HasPrefix(lower, "logs/") || lower == "split-tunnel.log") {
			continue
		}
		path := filepath.Join(root, rel)
		if err := noReparse(path); err != nil {
			return nil, err
		}
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return nil, fmt.Errorf("预期文件却发现目录：%s", path)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if !seen[strings.ToLower(path)] {
			paths = append(paths, path)
			seen[strings.ToLower(path)] = true
		}
	}
	return paths, nil
}

func validateRoot(root string) error {
	if !filepath.IsAbs(root) || strings.HasPrefix(root, `\\`) || filepath.Dir(root) == root {
		return errors.New("不能卸载根目录或网络目录")
	}
	for _, name := range []string{"SystemRoot", "ProgramFiles", "ProgramFiles(x86)", "ProgramData", "USERPROFILE", "PUBLIC"} {
		if value := os.Getenv(name); value != "" && strings.EqualFold(filepath.Clean(value), filepath.Clean(root)) {
			return errors.New("拒绝操作系统或用户公共目录")
		}
	}
	return noReparse(root)
}

func noReparse(path string) error {
	for {
		p, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return err
		}
		attrs, err := windows.GetFileAttributes(p)
		if err == nil && attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("拒绝操作符号链接或目录联接：%s", path)
		}
		if err != nil && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

func checkRoutes(root string) error {
	for _, rel := range []string{"data/applied_routes.json", "data/applied_vpn_routes.json"} {
		path := filepath.Join(root, rel)
		if err := noReparse(path); err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		var routes []string
		if err := json.Unmarshal(b, &routes); err != nil || len(routes) != 0 {
			return errors.New("检测到未清理或损坏的路由记录。请启动分流守卫，先断开连接并从托盘退出，再重试卸载；已保留恢复所需文件")
		}
	}
	return nil
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func runScript(script string) error {
	script = "[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)\n" + script
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("系统检查或清理失败：%w\n%s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func systemHooks() hooks {
	return hooks{
		preflight:    func(root string) error { return runScript(preflightScript(root, os.Getpid())) },
		integrations: func(root string) error { return runScript(integrationScript(root)) },
		unregister:   installation.Unregister,
		scheduleSelf: scheduleSelfRemoval,
	}
}

// A unique renamed path prevents reboot cleanup from deleting a later reinstall.
// If unregistering fails, put the executable back so the user can retry.
func scheduleSelfRemoval(path string) (func() error, error) {
	return scheduleRemoval(path, func(pending string) error {
		p, err := windows.UTF16PtrFromString(pending)
		if err != nil {
			return err
		}
		return windows.MoveFileEx(p, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
	})
}

func scheduleRemoval(path string, enqueue func(string) error) (func() error, error) {
	if err := noReparse(path); err != nil {
		return nil, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	pending := filepath.Join(filepath.Dir(path), fmt.Sprintf(".uninstall-%x.pending-delete.exe", nonce))
	if err := os.Rename(path, pending); err != nil {
		return nil, err
	}
	rollback := func() error { return os.Rename(pending, path) }
	if err := enqueue(pending); err != nil {
		return nil, errors.Join(err, rollback())
	}
	return rollback, nil
}

func preflightScript(root string, ownPID int) string {
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$root = %s
$processes = @(Get-CimInstance Win32_Process)
$blocked = @($processes | Where-Object { $_.ProcessId -ne %d -and $_.ExecutablePath -and $_.ExecutablePath.StartsWith($root + '\', [StringComparison]::OrdinalIgnoreCase) })
if ($blocked.Count -gt 0) { throw '程序或内置连接组件仍在运行。请先断开 VPN，并在托盘选择退出，再重试卸载。' }
# An orphaned dashboard may still write configuration; close it manually first.
foreach ($p in $processes) {
  if ($p.Name -notin @('powershell.exe','pwsh.exe')) { continue }
  $line = [string]$p.CommandLine
  if ($line.Contains((Join-Path $root 'data\dashboard-state.json'))) { throw '请先关闭分流管理台后重试。' }
  if ($line -match '(?i)-File\s+"?([^"\r\n]*anyconnect-dashboard-[^"\r\n ]+\.ps1)') {
    $scriptPath = $matches[1]
    if (Test-Path -LiteralPath $scriptPath -PathType Leaf) {
      $content = [IO.File]::ReadAllText($scriptPath)
      if ($content.Contains((Join-Path $root 'data\dashboard-state.json'))) { throw '请先关闭分流管理台后重试。' }
    }
  }
}`, psQuote(root), ownPID)
}

func integrationScript(root string) string {
	return integrationFunction + `
$ErrorActionPreference = 'Stop'
$target = ` + psQuote(filepath.Join(root, "anyconnect-split.exe")) + `
$service = New-Object -ComObject 'Schedule.Service'
$service.Connect()
$folder = $service.GetFolder('\')
$run = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Software\Microsoft\Windows\CurrentVersion\Run', $true)
$shell = New-Object -ComObject WScript.Shell
$folders = @([Environment]::GetFolderPath('CommonDesktopDirectory'), [Environment]::GetFolderPath('DesktopDirectory'), [Environment]::GetFolderPath('CommonPrograms'), [Environment]::GetFolderPath('Programs'))
try { Remove-OwnedIntegrations $target $folder $run $shell $folders } finally { if ($null -ne $run) { $run.Dispose() } }
`
}

const integrationFunction = `function Remove-OwnedIntegrations($target, $folder, $run, $shell, $folders) {
foreach ($task in $folder.GetTasks(0)) {
  if ($task.Name -ne 'AnyConnectSplitTunnel') { continue }
  $actions = @($task.Definition.Actions)
  if ($actions.Count -eq 1 -and $actions[0].Type -eq 0 -and $actions[0].Path.Trim('"') -ieq $target) { $folder.DeleteTask($task.Name, 0) }
}
if ($null -ne $run) {
    $value = [string]$run.GetValue('AnyConnectSplit', '')
    if ($value.Trim('"') -ieq $target) { $run.DeleteValue('AnyConnectSplit', $false) }
}
foreach ($dir in $folders) {
  if (-not $dir) { continue }
  foreach ($name in @('分流守卫.lnk','Split Tunnel.lnk')) {
    $path = Join-Path $dir $name
    if (Test-Path -LiteralPath $path -PathType Leaf) {
      if ($shell.CreateShortcut($path).TargetPath -ieq $target) { Remove-Item -LiteralPath $path -ErrorAction Stop }
    }
  }
}
}`
