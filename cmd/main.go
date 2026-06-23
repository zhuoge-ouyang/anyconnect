package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/user/anyconnect-split/internal/autostart"
	"github.com/user/anyconnect-split/internal/codexprobe"
	"github.com/user/anyconnect-split/internal/config"
	"github.com/user/anyconnect-split/internal/credential"
	"github.com/user/anyconnect-split/internal/dashboard"
	"github.com/user/anyconnect-split/internal/domainroute"
	"github.com/user/anyconnect-split/internal/ipdb"
	"github.com/user/anyconnect-split/internal/monitor"
	"github.com/user/anyconnect-split/internal/route"
	"github.com/user/anyconnect-split/internal/tray"
	"github.com/user/anyconnect-split/internal/tun"
	"github.com/user/anyconnect-split/internal/ui"
	"github.com/user/anyconnect-split/internal/vpn"
)

const credentialTarget = "AnyConnectSplitTunnel"
const desktopShortcutName = "分流守卫.lnk"
const legacyDesktopShortcutName = "Split Tunnel.lnk"
const shortcutIconFileName = "app-shortcut.ico"
const maxLogFileBytes = 20 * 1024 * 1024
const maxLogBackups = 3
const shortcutDescription = "分流守卫"

var (
	modUser32       = windows.NewLazySystemDLL("user32.dll")
	procFindWindowW = modUser32.NewProc("FindWindowW")
)

// waitForDesktopReady 等待 Windows 桌面就绪（Shell_TrayWnd 窗口出现）
// 最多等待 maxWait 时间，返回是否就绪
func waitForDesktopReady(maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	className, _ := windows.UTF16PtrFromString("Shell_TrayWnd")

	for time.Now().Before(deadline) {
		hwnd, _, _ := procFindWindowW.Call(
			uintptr(unsafe.Pointer(className)),
			0,
		)
		if hwnd != 0 {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func ensureSingleInstance() {
	name, _ := windows.UTF16PtrFromString("Global\\SplitTunnelMutex")
	_, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		os.Exit(0)
	}
}

func shouldRefreshAfterIPDBUpdate(splitEnabled bool, state monitor.VPNState, usingTun bool, tunActive bool) bool {
	if !splitEnabled {
		return false
	}
	if state != monitor.StateActive && state != monitor.StateConnected {
		return false
	}
	if usingTun && !tunActive {
		return false
	}
	return true
}

func ensureDesktopShortcut(ctx context.Context) {
	desktop := filepath.Join(os.Getenv("USERPROFILE"), "Desktop")
	shortcut := filepath.Join(desktop, desktopShortcutName)
	legacyShortcut := filepath.Join(desktop, legacyDesktopShortcutName)
	if _, err := os.Stat(shortcut); os.IsNotExist(err) {
		if _, legacyErr := os.Stat(legacyShortcut); legacyErr == nil {
			if renameErr := os.Rename(legacyShortcut, shortcut); renameErr != nil {
				log.Printf("Legacy desktop shortcut rename failed: %v", renameErr)
				shortcut = legacyShortcut
			}
		}
	}
	if _, err := os.Stat(shortcut); err == nil {
		return // 快捷方式已存在，跳过更新（避免开机自启时触发不必要的 COM 调用）
	}

	createOrUpdateShortcut(ctx, shortcut)
}

func shortcutIconPath(workDir string) string {
	return filepath.Join(workDir, shortcutIconFileName)
}

// COM GUIDs for IShellLink shortcut creation
var (
	clsidShellLink  = windows.GUID{Data1: 0x00021401, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIShellLinkW  = windows.GUID{Data1: 0x000214F9, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIPersistFile = windows.GUID{Data1: 0x0000010B, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
)

// createShortcutCOM creates or updates a .lnk shortcut using Windows COM API
// (IShellLink + IPersistFile), completely avoiding PowerShell execution.
func createShortcutCOM(shortcutPath, targetPath, workDir, iconPath, description string) error {
	ole32 := windows.NewLazySystemDLL("ole32.dll")
	procCoInit := ole32.NewProc("CoInitializeEx")
	procCoCreate := ole32.NewProc("CoCreateInstance")
	procCoUninit := ole32.NewProc("CoUninitialize")

	hr, _, _ := procCoInit.Call(0, 2) // COINIT_APARTMENTTHREADED
	if hr != 0 && hr != 1 {           // S_OK or S_FALSE (already initialized)
		return fmt.Errorf("CoInitializeEx failed: 0x%x", hr)
	}
	defer procCoUninit.Call()

	var pShellLink unsafe.Pointer
	hr, _, _ = procCoCreate.Call(
		uintptr(unsafe.Pointer(&clsidShellLink)),
		0,
		1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&iidIShellLinkW)),
		uintptr(unsafe.Pointer(&pShellLink)),
	)
	if hr != 0 {
		return fmt.Errorf("CoCreateInstance(IShellLink) failed: 0x%x", hr)
	}

	// Get vtable from COM object (use unsafe.Pointer to avoid go vet false positives)
	shellLink := uintptr(pShellLink)
	vtbl := (*[64]uintptr)(*(*unsafe.Pointer)(pShellLink))
	defer syscall.SyscallN(vtbl[2], shellLink) // IUnknown::Release

	// IShellLinkW::SetPath (vtable index 20)
	pTarget, _ := windows.UTF16PtrFromString(targetPath)
	if hr, _, _ = syscall.SyscallN(vtbl[20], shellLink, uintptr(unsafe.Pointer(pTarget))); hr != 0 {
		return fmt.Errorf("IShellLink::SetPath failed: 0x%x", hr)
	}

	// IShellLinkW::SetWorkingDirectory (vtable index 9)
	pWorkDir, _ := windows.UTF16PtrFromString(workDir)
	if hr, _, _ = syscall.SyscallN(vtbl[9], shellLink, uintptr(unsafe.Pointer(pWorkDir))); hr != 0 {
		return fmt.Errorf("IShellLink::SetWorkingDirectory failed: 0x%x", hr)
	}

	// IShellLinkW::SetDescription (vtable index 7)
	pDesc, _ := windows.UTF16PtrFromString(description)
	if hr, _, _ = syscall.SyscallN(vtbl[7], shellLink, uintptr(unsafe.Pointer(pDesc))); hr != 0 {
		return fmt.Errorf("IShellLink::SetDescription failed: 0x%x", hr)
	}

	// IShellLinkW::SetIconLocation (vtable index 17)
	pIcon, _ := windows.UTF16PtrFromString(iconPath)
	if hr, _, _ = syscall.SyscallN(vtbl[17], shellLink, uintptr(unsafe.Pointer(pIcon)), 0); hr != 0 {
		return fmt.Errorf("IShellLink::SetIconLocation failed: 0x%x", hr)
	}

	// QueryInterface for IPersistFile (vtable index 0)
	var pPersistFile unsafe.Pointer
	if hr, _, _ = syscall.SyscallN(vtbl[0], shellLink, uintptr(unsafe.Pointer(&iidIPersistFile)), uintptr(unsafe.Pointer(&pPersistFile))); hr != 0 {
		return fmt.Errorf("QueryInterface(IPersistFile) failed: 0x%x", hr)
	}
	persistFile := uintptr(pPersistFile)
	pfVtbl := (*[64]uintptr)(*(*unsafe.Pointer)(pPersistFile))
	defer syscall.SyscallN(pfVtbl[2], persistFile) // IPersistFile::Release

	// IPersistFile::Save (vtable index 6)
	pShortcut, _ := windows.UTF16PtrFromString(shortcutPath)
	if hr, _, _ = syscall.SyscallN(pfVtbl[6], persistFile, uintptr(unsafe.Pointer(pShortcut)), 1); hr != 0 {
		return fmt.Errorf("IPersistFile::Save failed: 0x%x", hr)
	}

	return nil
}

func createOrUpdateShortcut(_ context.Context, shortcut string) {
	exePath, _ := os.Executable()
	exePath, _ = filepath.EvalSymlinks(exePath)
	workDir := filepath.Dir(exePath)
	iconPath := shortcutIconPath(workDir)

	if err := createShortcutCOM(shortcut, exePath, workDir, iconPath, shortcutDescription); err != nil {
		log.Printf("Desktop shortcut creation failed: %v", err)
	}
}

func updateShortcut(_ context.Context, shortcut string) {
	exePath, _ := os.Executable()
	exePath, _ = filepath.EvalSymlinks(exePath)
	workDir := filepath.Dir(exePath)
	iconPath := shortcutIconPath(workDir)

	if err := createShortcutCOM(shortcut, exePath, workDir, iconPath, shortcutDescription); err != nil {
		log.Printf("Desktop shortcut update failed: %v", err)
	}
}

func ensureAppIcon() {
	iconPath := filepath.Join(baseDir(), "app.ico")
	if err := os.WriteFile(iconPath, tray.AppIcon, 0644); err != nil {
		log.Printf("Failed to write app icon: %v", err)
	}
	shortcutIcon := shortcutIconPath(baseDir())
	if err := os.WriteFile(shortcutIcon, tray.AppIcon, 0644); err != nil {
		log.Printf("Failed to write shortcut icon: %v", err)
	}
}

func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
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
	if err != nil {
		return false
	}
	return member
}

func baseDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

func rotateLogFile(path string, maxBytes int64, backups int) {
	if maxBytes <= 0 || backups <= 0 {
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() <= maxBytes {
		return
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", path, backups))
	for i := backups - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", path, i)
		newPath := fmt.Sprintf("%s.%d", path, i+1)
		if _, err := os.Stat(oldPath); err == nil {
			_ = os.Rename(oldPath, newPath)
		}
	}
	_ = os.Rename(path, path+".1")
}

// hideConsole 使用 FreeConsole 完全释放控制台，而不是隐藏窗口
// FreeConsole 不会影响后续 GUI 窗口（如 systray）的创建和消息循环
func hideConsole() {
	freeConsole := syscall.NewLazyDLL("kernel32.dll").NewProc("FreeConsole")
	freeConsole.Call()
}

// showErrorDialog 弹出错误对话框，确保用户能看到失败原因
// 具体使用 Windows MessageBox API 避免 PowerShell 特殊字符转义问题
func showErrorDialog(title, message string) {
	// 截断过长的消息
	if len(message) > 200 {
		message = message[:200] + "..."
	}
	user32 := syscall.NewLazyDLL("user32.dll")
	messageBox := user32.NewProc("MessageBoxW")
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	msgPtr, _ := syscall.UTF16PtrFromString(message)
	// MB_OK | MB_ICONERROR | MB_TOPMOST = 0x00000000 | 0x00000010 | 0x00040000
	messageBox.Call(0, uintptr(unsafe.Pointer(msgPtr)), uintptr(unsafe.Pointer(titlePtr)), 0x00040010)
}

func connectionFailureMessage(err error) string {
	if err == nil {
		return "连接失败，请重试。"
	}
	if errors.Is(err, tun.ErrOpenConnectAuthentication) {
		return "VPN 认证失败：请检查账号/密码是否正确，或确认该账号有当前节点权限。"
	}
	raw := err.Error()
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "sing-box exited before tun became ready") ||
		strings.Contains(lower, "cannot create a file when that file already exists") {
		return "分流内核启动失败：检测到上一次连接残留的 TUN 网卡或后台进程。\n\n程序已尝试自动清理，请重新连接一次；如果仍然失败，请先退出分流守卫再重新打开。"
	}
	return raw + "\n\n请换个节点重试。"
}

func shouldFallbackFromOpenConnect(err error, backend string) bool {
	if errors.Is(err, tun.ErrOpenConnectAuthentication) {
		return false
	}
	return backend != config.TrafficBackendOpenTun
}

func preferredSite(sites []ui.Site, preferred string) (ui.Site, bool) {
	if len(sites) == 0 {
		return ui.Site{}, false
	}
	for _, site := range sites {
		if site.Name == preferred {
			return site, true
		}
	}
	for _, site := range sites {
		if preferred != "" && strings.Contains(site.Name, preferred) {
			return site, true
		}
	}
	for _, hint := range []string{"香港", "台湾", "日本", "韩国", "美国", "英国", "加拿大", "澳大利亚"} {
		for _, site := range sites {
			if strings.Contains(site.Name, hint) {
				return site, true
			}
		}
	}
	for _, site := range sites {
		if !strings.Contains(site.Name, "国内") {
			return site, true
		}
	}
	return sites[0], true
}

func waitForLocalDefaultRoute(timeout time.Duration) (monitor.DefaultRoute, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		route, err := monitor.GetDefaultRoute()
		if err == nil {
			return route, nil
		}
		lastErr = err
		time.Sleep(1 * time.Second)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("default gateway not found")
	}
	return monitor.DefaultRoute{}, lastErr
}

func connectionInput(cfg *config.Config, sites []ui.Site, allowAuto bool) (ui.Site, string, string, bool) {
	if allowAuto && cfg.AutoConnect && cfg.SavedUsername != "" {
		site, ok := preferredSite(sites, cfg.PreferredSite)
		if ok {
			storedUsername, password, err := credential.Read(credentialTarget)
			if err == nil && password != "" {
				username := cfg.SavedUsername
				if username == "" {
					username = storedUsername
				}
				log.Printf("Auto-connecting to configured site: %s", site.Name)
				return site, username, password, true
			}
			log.Printf("Auto-connect credentials unavailable: %v", err)
		}
	}

	result := ui.ShowLoginDialog(sites, cfg.PreferredSite, cfg.SavedUsername, cfg.AutoConnect)
	if !result.OK {
		return ui.Site{}, "", "", false
	}

	cfg.PreferredSite = result.SiteName
	if result.Remember {
		cfg.AutoConnect = true
		cfg.SavedUsername = result.Username
		if err := credential.Write(credentialTarget, result.Username, result.Password); err != nil {
			cfg.AutoConnect = false
			log.Printf("Failed to save VPN credential: %v", err)
		}
	} else {
		cfg.AutoConnect = false
		cfg.SavedUsername = ""
		_ = credential.Delete(credentialTarget)
	}
	cfg.Save()

	return ui.Site{Name: result.SiteName, Server: result.Server}, result.Username, result.Password, true
}

func storedCredential(cfg *config.Config) (string, string, error) {
	storedUsername, password, err := credential.Read(credentialTarget)
	if err != nil {
		return "", "", err
	}
	username := cfg.SavedUsername
	if username == "" {
		username = storedUsername
	}
	if username == "" || password == "" {
		return "", "", fmt.Errorf("saved VPN credential is incomplete")
	}
	return username, password, nil
}

func findSite(sites []ui.Site, name string) (ui.Site, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return ui.Site{}, false
	}
	for _, site := range sites {
		if site.Name == name {
			return site, true
		}
	}
	for _, site := range sites {
		if strings.Contains(site.Name, name) || strings.Contains(name, site.Name) {
			return site, true
		}
	}
	return ui.Site{}, false
}

func uniqueSites(sites []ui.Site) []ui.Site {
	seen := make(map[string]struct{}, len(sites))
	unique := make([]ui.Site, 0, len(sites))
	for _, site := range sites {
		key := site.Server
		if key == "" {
			key = site.Name
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, site)
	}
	return unique
}

func codexCandidateSites(cfg *config.Config, sites []ui.Site) []ui.Site {
	names := cfg.CodexCandidateSites
	if !cfg.CodexAutoSelect {
		names = []string{cfg.CodexPreferredSite}
	}

	candidates := make([]ui.Site, 0, len(names)+1)
	for _, name := range names {
		if site, ok := findSite(sites, name); ok {
			candidates = append(candidates, site)
		}
	}
	if site, ok := findSite(sites, cfg.CodexPreferredSite); ok {
		candidates = append(candidates, site)
	}
	return uniqueSites(candidates)
}

func probeCodex(attempts int) []codexprobe.Result {
	if attempts <= 0 {
		attempts = 1
	}
	results := make([]codexprobe.Result, 0, attempts)
	for i := 0; i < attempts; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		results = append(results, codexprobe.Probe(ctx))
		cancel()
		time.Sleep(500 * time.Millisecond)
	}
	return results
}

func serverHostname(server string) string {
	u, err := url.Parse(server)
	if err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	host := strings.TrimSpace(server)
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.IndexAny(host, "/:"); i >= 0 {
		host = host[:i]
	}
	return host
}

func resolveServerIPs(server string) []net.IP {
	host := serverHostname(server)
	if host == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		log.Printf("Could not resolve VPN server %s for route protection: %v", host, err)
		return nil
	}
	ips := make([]net.IP, 0, len(addrs))
	seen := make(map[string]struct{}, len(addrs))
	for _, addr := range addrs {
		if addr.IP == nil {
			continue
		}
		key := addr.IP.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ips = append(ips, addr.IP)
	}
	return ips
}

func ipStrings(ips []net.IP) []string {
	result := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip != nil {
			result = append(result, ip.String())
		}
	}
	return result
}

func appendUniqueIPs(dst []net.IP, src []net.IP) []net.IP {
	seen := make(map[string]struct{}, len(dst)+len(src))
	result := make([]net.IP, 0, len(dst)+len(src))
	for _, ip := range append(dst, src...) {
		if ip == nil {
			continue
		}
		key := ip.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, ip)
	}
	return result
}

func main() {
	// 1. 管理员权限检查
	if !isAdmin() {
		exe, _ := os.Executable()
		workDir := filepath.Dir(exe)
		verb := "runas"
		verbPtr, _ := windows.UTF16PtrFromString(verb)
		exePtr, _ := windows.UTF16PtrFromString(exe)
		cwdPtr, _ := windows.UTF16PtrFromString(workDir)
		windows.ShellExecute(0, verbPtr, exePtr, nil, cwdPtr, windows.SW_NORMAL)
		os.Exit(0)
	}

	// 2. 单实例检查
	ensureSingleInstance()

	// 隐藏控制台窗口（替代 -H windowsgui，避免 systray 兼容性问题）
	hideConsole()

	// 3. 日志初始化
	logFile := filepath.Join(baseDir(), "split-tunnel.log")
	rotateLogFile(logFile, maxLogFileBytes, maxLogBackups)
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	log.Println("AnyConnect Split Tunnel starting...")
	ensureAppIcon()

	// 4. 加载配置
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Warning: failed to load config, using defaults: %v", err)
		cfg = config.DefaultConfig()
	}
	if actualAutoStart := autostart.IsEnabled(); cfg.AutoStart != actualAutoStart {
		cfg.AutoStart = actualAutoStart
		cfg.Save()
	}

	// 5. 检测 vpncli.exe 路径
	cliPath := cfg.VPNCLIPath
	if cliPath == "" {
		cliPath = config.DetectVPNCLIPath()
		if cliPath == "" {
			log.Println("Warning: vpncli.exe not found; OpenConnect TUN will be tried before any Cisco fallback")
			if cfg.TrafficBackend == config.TrafficBackendCiscoStatic {
				showErrorDialog("错误", "未找到 vpncli.exe，请在配置文件中设置 vpncli_path")
				os.Exit(1)
			}
		} else {
			log.Printf("Detected vpncli.exe: %s", cliPath)
		}
	}

	// 6. 等待 Windows 桌面就绪后再启动托盘
	log.Println("Waiting for Windows desktop ready (Shell_TrayWnd)...")
	if !waitForDesktopReady(60 * time.Second) {
		log.Println("WARNING: Desktop not ready after 60s, starting tray anyway...")
	} else {
		log.Println("Desktop ready, starting tray...")
	}

	dataDir := filepath.Join(baseDir(), "data")
	dashboardStore := dashboard.NewStore(dataDir)
	dashboardController := dashboard.NewController(dashboardStore, dashboard.Actions{})
	dashboardBackend := func() string { return "" }
	dashboardRouteCount := func() int { return 0 }
	publishDashboard := func(status tray.Status) {
		routeCount := status.RouteCount
		if routeCount == 0 {
			routeCount = dashboardRouteCount()
		}
		backend := dashboardBackend()
		if backend == "" {
			backend = "未连接"
		}
		if err := dashboardController.UpdateSnapshot(dashboard.Snapshot{
			StatusText:          status.StatusText,
			CurrentSite:         status.CurrentSite,
			SplitTunnelEnabled:  status.SplitEnabled,
			AutoStartEnabled:    status.AutoStart,
			Backend:             backend,
			RouteCount:          routeCount,
			LastIPDBUpdate:      cfg.LastUpdate,
			OriginalGateway:     cfg.OriginalGateway,
			OriginalInterface:   cfg.OriginalInterfaceIndex,
			OriginalIPv6Gateway: cfg.OriginalIPv6Gateway,
			OriginalIPv6IfIndex: cfg.OriginalIPv6Interface,
			IPv6SplitEnabled:    cfg.IPv6SplitEnabled,
			LastError:           status.LastError,
		}); err != nil {
			log.Printf("Failed to write dashboard snapshot: %v", err)
		}
	}
	showDashboard := func() {
		if err := ui.ShowDashboard(dashboardStore, filepath.Join(baseDir(), "app.ico")); err != nil {
			log.Printf("Failed to show dashboard: %v", err)
		}
	}
	trayUI := tray.New(tray.Actions{
		OnOpenDashboard: showDashboard,
		OnContactAuthor: tray.ShowContactAuthor,
		OnQuit: func() {
			log.Println("Quitting (early)...")
			os.Exit(0)
		},
		OnViewLog: func() {
			exec.Command("notepad", logFile).Start()
		},
	}, cfg.SplitTunnelEnabled, cfg.AutoStart)
	trayUI.SetStatusListener(publishDashboard)
	trayUI.SetInitialTooltip("AnyConnect Split Tunnel - 初始化中...")
	go func() {
		trayUI.Run()
		os.Exit(0)
	}()
	trayUI.WaitReady()
	log.Println("Tray initialized successfully")
	trayUI.SetStatusBusy("正在初始化...")

	// 7. 清理残留 Cisco 会话
	if cliPath != "" {
		vpn.PrepareForNewConnection(cliPath)
	}

	// 8. 并发初始化: IPv4/IPv6 网关检测 + IP 数据库下载 + 桌面快捷方式 + 站点 IP 预解析
	sites := make([]ui.Site, len(cfg.VPNSites))
	for i, s := range cfg.VPNSites {
		sites[i] = ui.Site{Name: s.Name, Server: s.Server}
	}
	protectedServerIPs := make(map[string][]net.IP)
	var protectedServerMu sync.Mutex
	cacheServerIPs := func(site ui.Site) []net.IP {
		if site.Server == "" {
			return nil
		}
		protectedServerMu.Lock()
		cached := append([]net.IP(nil), protectedServerIPs[site.Server]...)
		protectedServerMu.Unlock()
		if len(cached) > 0 {
			return cached
		}
		ips := resolveServerIPs(site.Server)
		if len(ips) == 0 {
			return nil
		}
		protectedServerMu.Lock()
		protectedServerIPs[site.Server] = appendUniqueIPs(protectedServerIPs[site.Server], ips)
		cached = append([]net.IP(nil), protectedServerIPs[site.Server]...)
		protectedServerMu.Unlock()
		return cached
	}
	allProtectedServerIPs := func(current ui.Site) []net.IP {
		ips := cacheServerIPs(current)
		protectedServerMu.Lock()
		for _, cached := range protectedServerIPs {
			ips = appendUniqueIPs(ips, cached)
		}
		protectedServerMu.Unlock()
		return ips
	}

	// 8a. IPv4 + IPv6 网关并发检测
	var gatewayWg sync.WaitGroup
	gatewayWg.Add(2)
	go func() {
		defer gatewayWg.Done()
		if r, err := waitForLocalDefaultRoute(15 * time.Second); err != nil {
			log.Printf("Warning: could not detect local default route before VPN connect: %v", err)
		} else {
			cfg.OriginalGateway = r.Gateway
			cfg.OriginalInterfaceIndex = r.InterfaceIndex
			log.Printf("Detected local default route: gateway=%s interface_ip=%s interface_index=%d",
				r.Gateway, r.InterfaceIP, r.InterfaceIndex)
		}
	}()
	go func() {
		defer gatewayWg.Done()
		if r, err := monitor.GetDefaultIPv6Route(); err != nil {
			log.Printf("IPv6 local default route unavailable, IPv6 split routes will be skipped: %v", err)
			cfg.OriginalIPv6Gateway = ""
			cfg.OriginalIPv6Interface = 0
		} else {
			cfg.OriginalIPv6Gateway = r.Gateway
			cfg.OriginalIPv6Interface = r.InterfaceIndex
			log.Printf("Detected local IPv6 default route: gateway=%s interface_index=%d",
				r.Gateway, r.InterfaceIndex)
		}
	}()

	// 8b. IP 数据库异步下载（最多等 10 秒）
	db := ipdb.New(dataDir)
	ipdbReady := make(chan struct{})
	go func() {
		defer close(ipdbReady)
		if db.NeedsUpdate() {
			log.Println("IP database updating in background...")
			if _, err := db.Update(); err != nil {
				log.Printf("Failed to download IP database: %v", err)
			} else {
				cfg.LastUpdate = time.Now()
				cfg.Save()
				log.Println("IP database downloaded successfully")
			}
		}
	}()

	// 8c. 桌面快捷方式（后台, 5 秒超时，失败仅 log）
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ensureDesktopShortcut(ctx)
	}()

	// 8d. 并发预解析所有站点 VPN 服务器 IP
	go func() {
		var resolveWg sync.WaitGroup
		for _, site := range sites {
			if site.Server == "" {
				continue
			}
			resolveWg.Add(1)
			go func(s ui.Site) {
				defer resolveWg.Done()
				cacheServerIPs(s)
			}(site)
		}
		resolveWg.Wait()
		log.Printf("Pre-resolved %d site server IPs", len(sites))
	}()

	// 等待网关检测完成（TUN session 和路由管理依赖网关信息）
	gatewayWg.Wait()
	cfg.Save()

	// 等待 IPDB（最多 10 秒后继续启动，下载在后台继续）
	select {
	case <-ipdbReady:
		log.Println("IP database ready")
	case <-time.After(10 * time.Second):
		log.Println("IP database still downloading, continuing startup...")
	}

	trayUI.SetStatusBusy("等待用户登录...")

	// 创建 TUN session（需要网关信息就绪）
	tunSession := tun.New(tun.Options{
		OpenConnectPath:     cfg.OpenConnectPath,
		SingBoxPath:         cfg.SingBoxPath,
		DataDir:             dataDir,
		LocalGateway:        cfg.OriginalGateway,
		LocalInterfaceIndex: cfg.OriginalInterfaceIndex,
		DirectDomains:       append([]string{"cn"}, cfg.DomesticDomains...),
		SingBoxLogLevel:     cfg.LogLevel,
	})
	var sessionMu sync.Mutex
	activeBackend := ""
	setActiveBackend := func(backend string) {
		sessionMu.Lock()
		activeBackend = backend
		sessionMu.Unlock()
	}
	currentBackend := func() string {
		sessionMu.Lock()
		defer sessionMu.Unlock()
		return activeBackend
	}
	dashboardBackend = currentBackend
	usingTun := func() bool {
		return currentBackend() == config.TrafficBackendOpenTun
	}
	protectedServerCIDRs := func(site ui.Site) []string {
		ips := cacheServerIPs(site)
		cidrs := make([]string, 0, len(ips))
		for _, ip := range ips {
			if ip == nil {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				cidrs = append(cidrs, v4.String()+"/32")
				continue
			}
			if cfg.IPv6SplitEnabled {
				if v6 := ip.To16(); v6 != nil {
					cidrs = append(cidrs, v6.String()+"/128")
				}
			}
		}
		return route.SummarizeCIDRs(cidrs)
	}
	loadSplitCIDRsForSite := func(site ui.Site, showProgress bool, includeDomainExceptions bool) ([]string, error) {
		cidrs, err := db.Load()
		if err != nil {
			return nil, err
		}
		cidrs = route.SummarizeCIDRs(cidrs)
		if len(cidrs) == 0 {
			return cidrs, nil
		}

		if !cfg.IPv6SplitEnabled {
			before := len(cidrs)
			cidrs = route.IPv4Only(cidrs)
			if before != len(cidrs) {
				log.Printf("IPv6 split routes disabled, skipped %d IPv6 route(s)", before-len(cidrs))
			}
		}

		protectedIPs := allProtectedServerIPs(site)
		if len(protectedIPs) > 0 {
			filtered, excluded := route.ExcludeCIDRsContainingIPs(cidrs, protectedIPs)
			if len(excluded) > 0 {
				log.Printf("Excluded %d route(s) containing VPN server IP(s) from local split bypass: current_site=%s ips=%v cidrs=%v",
					len(excluded), site.Name, ipStrings(protectedIPs), excluded)
				cidrs = filtered
			}
		}

		if includeDomainExceptions && len(cfg.DomesticDomains) > 0 {
			if showProgress && trayUI != nil {
				trayUI.SetStatusBusy("正在解析国内域名例外...")
			}
			domainCIDRs := domainroute.Resolve(cfg.DomesticDomains)
			if len(domainCIDRs) > 0 {
				log.Printf("Resolved %d domestic domain exception routes", len(domainCIDRs))
				cidrs = append(cidrs, domainCIDRs...)
			}
		}
		return route.SummarizeCIDRs(cidrs), nil
	}
	tunDirectCIDRsForSite := func(site ui.Site, showProgress bool) ([]string, []string, error) {
		protected := protectedServerCIDRs(site)
		if !cfg.SplitTunnelEnabled {
			return nil, protected, nil
		}
		cidrs, err := loadSplitCIDRsForSite(site, showProgress, true)
		if err != nil {
			return nil, protected, err
		}
		return cidrs, protected, nil
	}
	connectWithBestBackend := func(site ui.Site, username, password string) (string, error) {
		if cfg.TrafficBackend != config.TrafficBackendCiscoStatic {
			directCIDRs, protectedCIDRs, err := tunDirectCIDRsForSite(site, false)
			if err != nil {
				log.Printf("OpenConnect TUN skipped, could not load split rules: %v", err)
			} else if err := tunSession.Start(context.Background(), tun.StartOptions{
				Server:         site.Server,
				Username:       username,
				Password:       password,
				DirectCIDRs:    directCIDRs,
				ProtectedCIDRs: protectedCIDRs,
				SplitEnabled:   cfg.SplitTunnelEnabled,
			}); err == nil {
				setActiveBackend(config.TrafficBackendOpenTun)
				return config.TrafficBackendOpenTun, nil
			} else {
				_ = tunSession.Stop()
				if !shouldFallbackFromOpenConnect(err, cfg.TrafficBackend) {
					log.Printf("OpenConnect TUN failed without fallback: %v", err)
					return "", err
				}
				log.Printf("OpenConnect TUN unavailable, falling back to Cisco static backend: %v", err)
			}
		}

		if cliPath == "" {
			return "", fmt.Errorf("vpncli.exe not found and OpenConnect TUN is unavailable")
		}
		vpn.PrepareForNewConnection(cliPath)
		if err := vpn.Connect(cliPath, site.Server, username, password); err != nil {
			return "", err
		}
		setActiveBackend(config.TrafficBackendCiscoStatic)
		return config.TrafficBackendCiscoStatic, nil
	}
	allowAutoConnect := true
	var connectedSite ui.Site
	for {
		selectedSite, username, password, ok := connectionInput(cfg, sites, allowAutoConnect)
		if !ok {
			log.Println("User cancelled login dialog")
			os.Exit(0)
		}
		log.Printf("User selected site: %s (%s), username: %s", selectedSite.Name, selectedSite.Server, username)
		cacheServerIPs(selectedSite)

		// 9. 连接 VPN
		log.Println("Connecting to VPN...")
		backend, err := connectWithBestBackend(selectedSite, username, password)
		if err != nil {
			log.Printf("VPN connection failed: %v", err)
			showErrorDialog("连接失败", connectionFailureMessage(err))
			allowAutoConnect = false
			continue
		}
		log.Printf("VPN connected with backend: %s", backend)
		connectedSite = selectedSite
		break
	}
	log.Println("VPN connected successfully")

	// Initialize route manager
	routeMgr := route.NewManager(
		cfg.OriginalGateway,
		cfg.OriginalInterfaceIndex,
		cfg.OriginalIPv6Gateway,
		cfg.OriginalIPv6Interface,
		dataDir,
	)
	dashboardRouteCount = routeMgr.GetAppliedRouteCount
	// 注意：CleanupStaleRoutes 移至 tray 启动后异步执行，避免阻塞主 goroutine

	// 10. 初始化 VPN 监控
	var suppressVPNDetection atomic.Bool
	vpnMon := monitor.NewWithStatusDetector(func() monitor.VPNPresence {
		if suppressVPNDetection.Load() {
			return monitor.PresenceUnknown
		}
		if usingTun() {
			return tunSession.Presence()
		}
		if cliPath == "" {
			return monitor.PresenceUnknown
		}
		return vpn.ConnectionPresence(cliPath, 5*time.Second)
	})
	var routeApplyMu sync.Mutex
	var disconnectMu sync.Mutex
	var modeSwitchMu sync.Mutex
	var ipdbUpdateMu sync.Mutex
	var routeOpsMu sync.Mutex
	routeOpsCtx, cancelRouteOps := context.WithCancel(context.Background())

	cancelActiveRouteOps := func() {
		routeOpsMu.Lock()
		cancelRouteOps()
		routeOpsMu.Unlock()
	}

	resetRouteOps := func() {
		routeOpsMu.Lock()
		cancelRouteOps()
		routeOpsCtx, cancelRouteOps = context.WithCancel(context.Background())
		routeOpsMu.Unlock()
	}

	currentRouteOpsContext := func() context.Context {
		routeOpsMu.Lock()
		defer routeOpsMu.Unlock()
		return routeOpsCtx
	}

	targetBaseSplitCIDRs := func() ([]string, error) {
		return loadSplitCIDRsForSite(connectedSite, false, false)
	}

	targetSplitCIDRs := func(showProgress bool) ([]string, error) {
		return loadSplitCIDRsForSite(connectedSite, showProgress, true)
	}

	routeProgress := func(done, total int) {
		if trayUI != nil && total > 0 && done > 0 {
			trayUI.SetStatusBusy(fmt.Sprintf("正在写入路由 %d/%d...", done, total))
		}
	}

	cleanupProgress := func(done, total int) {
		if trayUI != nil && total > 0 && done > 0 {
			trayUI.SetStatusBusy(fmt.Sprintf("正在清理路由 %d/%d...", done, total))
		}
	}

	applySplitRoutes := func(forceRefresh bool) {
		routeApplyMu.Lock()
		defer routeApplyMu.Unlock()

		ctx := currentRouteOpsContext()
		if err := ctx.Err(); err != nil {
			log.Printf("Skipping route application, route task is canceled: %v", err)
			return
		}
		if usingTun() {
			if forceRefresh {
				directCIDRs, protectedCIDRs, err := tunDirectCIDRsForSite(connectedSite, true)
				if err != nil {
					log.Printf("Error loading TUN split rules: %v", err)
					trayUI.SetStatusError("加载 TUN 规则失败")
					return
				}
				if trayUI != nil {
					trayUI.SetStatusBusy("正在刷新 TUN 分流规则...")
				}
				if err := tunSession.Refresh(ctx, directCIDRs, protectedCIDRs, cfg.SplitTunnelEnabled); err != nil {
					log.Printf("TUN refresh failed: %v", err)
					trayUI.SetStatusError("TUN 分流刷新失败")
					return
				}
			}
			if cfg.SplitTunnelEnabled {
				trayUI.SetStatusTunActive()
			} else {
				trayUI.SetStatusTunFullTunnel()
			}
			vpnMon.SetState(monitor.StateActive)
			return
		}
		if !cfg.SplitTunnelEnabled {
			trayUI.SetStatusSplitDisabled()
			vpnMon.SetState(monitor.StateActive)
			return
		}
		if routeMgr.HasAppliedRoutes() && !forceRefresh {
			trayUI.SetStatusActive(routeMgr.GetAppliedRouteCount())
			vpnMon.SetState(monitor.StateActive)
			return
		}
		if cfg.OriginalGateway == "" {
			log.Println("Error: no original gateway configured")
			trayUI.SetStatusError("No gateway configured")
			return
		}

		trayUI.SetStatusBusy("正在加载路由...")
		cidrs, err := targetSplitCIDRs(true)
		if err != nil {
			log.Printf("Error loading IP list: %v", err)
			trayUI.SetStatusError("加载 IP 库失败")
			return
		}
		if len(cidrs) == 0 {
			log.Println("Warning: IP database is empty, no routes to apply")
			trayUI.SetStatusError("IP 库为空")
			return
		}

		if forceRefresh && routeMgr.HasAppliedRoutes() {
			trayUI.SetStatusBusy("正在刷新旧路由...")
			removed, removeErrors := routeMgr.RemoveAllRoutesContext(ctx, cleanupProgress)
			if err := ctx.Err(); err != nil {
				log.Printf("Split tunnel route refresh canceled during cleanup: %v", err)
				return
			}
			log.Printf("Routes cleaned before refresh: %d removed, %d errors", removed, removeErrors)
		}

		trayUI.SetStatusBusy(fmt.Sprintf("正在加载 %d 条路由...", len(cidrs)))
		trayUI.SetStatusBusy(fmt.Sprintf("正在批量写入 %d 条路由...", len(cidrs)))
		added, errors := routeMgr.AddRoutesContext(ctx, cidrs, routeProgress)
		if err := ctx.Err(); err != nil {
			log.Printf("Split tunnel route application canceled: %v", err)
			return
		}
		log.Printf("Split tunnel routes applied: %d added, %d errors", added, errors)
		if currentBackend() == config.TrafficBackendCiscoStatic && cfg.TrafficBackend != config.TrafficBackendCiscoStatic {
			trayUI.SetStatusStaticFallback(routeMgr.GetAppliedRouteCount())
		} else {
			trayUI.SetStatusActive(routeMgr.GetAppliedRouteCount())
		}
		vpnMon.SetState(monitor.StateActive)
	}

	disconnectSession := func(reason string) error {
		disconnectMu.Lock()
		defer disconnectMu.Unlock()

		log.Printf("Disconnect requested: %s", reason)
		cancelActiveRouteOps()
		if trayUI != nil {
			trayUI.SetStatusBusy("正在断开 VPN...")
		}
		if usingTun() {
			if err := tunSession.Stop(); err != nil {
				log.Printf("OpenConnect TUN disconnect failed: %v", err)
			}
		}
		removed, removeErrors := routeMgr.RemoveAllRoutesContext(context.Background(), cleanupProgress)
		log.Printf("Routes cleaned before disconnect: %d removed, %d errors", removed, removeErrors)
		var err error
		if cliPath != "" && currentBackend() == config.TrafficBackendCiscoStatic {
			err = vpn.Disconnect(cliPath)
			if err != nil {
				log.Printf("VPN disconnect failed: %v", err)
			}
		}
		setActiveBackend("")
		vpnMon.SetState(monitor.StateIdle)
		if trayUI != nil {
			trayUI.ClearCurrentSite()
			trayUI.SetStatusIdle()
		}
		resetRouteOps()
		return err
	}

	connectFinalSite := func(site ui.Site, username, password, reason string) error {
		log.Printf("Connecting final site for %s: %s (%s)", reason, site.Name, site.Server)
		if trayUI != nil {
			trayUI.SetStatusBusy("正在连接 " + site.Name + "...")
		}
		cacheServerIPs(site)
		resetRouteOps()
		backend, err := connectWithBestBackend(site, username, password)
		if err != nil {
			cancelActiveRouteOps()
			if trayUI != nil {
				trayUI.SetStatusError("连接失败")
			}
			return err
		}
		log.Printf("Connected final site with backend: %s", backend)
		if trayUI != nil {
			trayUI.SetCurrentSite(site.Name)
			trayUI.SetStatusBusy("正在恢复分流...")
		}
		connectedSite = site
		suppressVPNDetection.Store(false)
		vpnMon.SetState(monitor.StateConnected)
		return nil
	}

	restoreNormalSite := func(reason string) {
		go func() {
			modeSwitchMu.Lock()
			defer modeSwitchMu.Unlock()

			username, password, err := storedCredential(cfg)
			if err != nil {
				log.Printf("Restore normal site skipped, saved credential unavailable: %v", err)
				if trayUI != nil {
					trayUI.SetStatusError("缺少已保存凭据")
				}
				return
			}
			site, ok := preferredSite(sites, cfg.PreferredSite)
			if !ok {
				log.Printf("Restore normal site skipped, preferred site unavailable: %q", cfg.PreferredSite)
				if trayUI != nil {
					trayUI.SetStatusError("常用节点不存在")
				}
				return
			}

			suppressVPNDetection.Store(true)
			defer suppressVPNDetection.Store(false)
			_ = disconnectSession(reason)
			if err := connectFinalSite(site, username, password, reason); err != nil {
				log.Printf("Restore normal site failed: %v", err)
				if trayUI != nil {
					trayUI.SetStatusError("恢复常用线路失败")
				}
			}
		}()
	}

	switchToCodexMode := func() {
		go func() {
			modeSwitchMu.Lock()
			defer modeSwitchMu.Unlock()

			username, password, err := storedCredential(cfg)
			if err != nil {
				log.Printf("Codex mode skipped, saved credential unavailable: %v", err)
				if trayUI != nil {
					trayUI.SetStatusError("缺少已保存凭据")
				}
				return
			}
			candidates := codexCandidateSites(cfg, sites)
			if len(candidates) == 0 {
				log.Println("Codex mode skipped, no candidate sites configured")
				if trayUI != nil {
					trayUI.SetStatusError("无 Codex 候选节点")
				}
				return
			}
			normalSite, hasNormalSite := preferredSite(sites, cfg.PreferredSite)

			type candidateResult struct {
				site    ui.Site
				results []codexprobe.Result
				score   int
			}
			var best candidateResult
			best.score = -1 << 30
			hasBest := false

			suppressVPNDetection.Store(true)
			defer suppressVPNDetection.Store(false)
			_ = disconnectSession("codex mode")

			for i, site := range candidates {
				if trayUI != nil {
					trayUI.SetStatusBusy(fmt.Sprintf("Codex 测试 %d/%d：%s", i+1, len(candidates), site.Name))
				}
				_ = disconnectSession("codex candidate")
				if trayUI != nil {
					trayUI.ClearCurrentSite()
				}
				log.Printf("Codex candidate connecting: %s (%s)", site.Name, site.Server)
				cacheServerIPs(site)
				backend, err := connectWithBestBackend(site, username, password)
				if err != nil {
					log.Printf("Codex candidate connect failed for %s: %v", site.Name, err)
					continue
				}
				log.Printf("Codex candidate connected with backend: %s", backend)
				connectedSite = site
				if trayUI != nil {
					trayUI.SetCurrentSite(site.Name)
				}

				results := probeCodex(cfg.CodexProbeAttempts)
				score := codexprobe.Score(results)
				healthy := codexprobe.AllHealthy(results)
				for n, result := range results {
					log.Printf(
						"Codex probe %s #%d: status=%d duration=%s cf_ray=%s mitigated=%s err=%s",
						site.Name,
						n+1,
						result.Status,
						result.Duration.Round(time.Millisecond),
						result.CFRay,
						result.CFMitigated,
						result.Error,
					)
				}
				log.Printf("Codex candidate result: site=%s healthy=%v score=%d median=%s",
					site.Name, healthy, score, codexprobe.MedianDuration(results).Round(time.Millisecond))

				if healthy && score > best.score {
					best = candidateResult{site: site, results: results, score: score}
					hasBest = true
				}
				if healthy && !cfg.CodexAutoSelect {
					break
				}
			}

			if !hasBest {
				log.Println("Codex mode found no healthy site")
				if hasNormalSite {
					_ = disconnectSession("restore after codex probe failure")
					if err := connectFinalSite(normalSite, username, password, "restore after codex probe failure"); err != nil {
						log.Printf("Failed to restore normal site after Codex probe failure: %v", err)
					}
				}
				if trayUI != nil {
					trayUI.SetStatusError("Codex 节点检测失败")
				}
				return
			}

			_ = disconnectSession("codex final")
			if err := connectFinalSite(best.site, username, password, "codex mode final"); err != nil {
				log.Printf("Codex final connect failed for %s: %v", best.site.Name, err)
				if hasNormalSite {
					_ = disconnectSession("restore after codex final failure")
					if restoreErr := connectFinalSite(normalSite, username, password, "restore after codex final failure"); restoreErr != nil {
						log.Printf("Failed to restore normal site after Codex final failure: %v", restoreErr)
					}
				}
				if trayUI != nil {
					trayUI.SetStatusError("Codex 线路连接失败")
				}
				return
			}

			cfg.CodexPreferredSite = best.site.Name
			_ = cfg.Save()
			log.Printf("Codex mode selected site: %s score=%d median=%s",
				best.site.Name, best.score, codexprobe.MedianDuration(best.results).Round(time.Millisecond))
		}()
	}

	// Define tray actions
	actions := tray.Actions{
		OnDisconnect: func() error {
			return disconnectSession("tray menu")
		},
		OnReconnect: func() {
			log.Println("Reconnecting VPN...")
			disconnectSession("reconnect")
			selectedSite, username, password, ok := connectionInput(cfg, sites, false)
			if !ok {
				log.Println("User cancelled reconnect dialog")
				return
			}
			resetRouteOps()
			log.Printf("Reconnecting to %s (%s) as %s", selectedSite.Name, selectedSite.Server, username)
			cacheServerIPs(selectedSite)
			backend, err := connectWithBestBackend(selectedSite, username, password)
			if err != nil {
				cancelActiveRouteOps()
				log.Printf("Reconnect failed: %v", err)
				return
			}
			log.Printf("Reconnected with backend: %s", backend)
			connectedSite = selectedSite
			trayUI.SetCurrentSite(selectedSite.Name)
			vpnMon.SetState(monitor.StateConnected)
		},
		OnCodexMode: func() {
			log.Println("Codex stable mode requested")
			switchToCodexMode()
		},
		OnRestoreNormal: func() {
			log.Println("Restore normal site requested")
			restoreNormalSite("restore normal")
		},
		OnToggleSplit: func(enabled bool) {
			trayUI.SetSplitEnabled(enabled)
			cfg.SplitTunnelEnabled = enabled
			cfg.Save()
			if usingTun() {
				resetRouteOps()
				applySplitRoutes(true)
				return
			}
			if !enabled {
				cancelActiveRouteOps()
				if routeMgr.HasAppliedRoutes() {
					routeMgr.RemoveAllRoutesContext(context.Background(), cleanupProgress)
					log.Println("Split tunnel disabled, routes removed")
				}
				trayUI.SetStatusSplitDisabled()
				return
			}
			if enabled {
				resetRouteOps()
				applySplitRoutes(false)
			}
		},
		OnUpdateIPDB: func() {
			go func() {
				ipdbUpdateMu.Lock()
				defer ipdbUpdateMu.Unlock()

				log.Println("Manual IP database update requested")
				if trayUI != nil {
					trayUI.SetStatusBusy("正在更新 IP 数据库...")
				}
				if _, err := db.Update(); err != nil {
					log.Printf("Update failed: %v", err)
					if trayUI != nil {
						trayUI.SetStatusError("IP 库更新失败")
					}
					return
				}

				cfg.LastUpdate = time.Now()
				cfg.Save()
				log.Println("IP database updated successfully")
				if trayUI != nil {
					trayUI.SetStatusBusy("IP 数据库已更新")
				}

				state := vpnMon.State()
				tunBackend := usingTun()
				tunActive := !tunBackend || tunSession.Active()
				if shouldRefreshAfterIPDBUpdate(cfg.SplitTunnelEnabled, state, tunBackend, tunActive) {
					if trayUI != nil {
						if tunBackend {
							trayUI.SetStatusBusy("IP 数据库已更新，正在刷新 TUN 规则...")
						} else {
							trayUI.SetStatusBusy("IP 数据库已更新，正在刷新路由...")
						}
					}
					resetRouteOps()
					applySplitRoutes(true)
				} else if cfg.SplitTunnelEnabled && tunBackend && !tunActive && trayUI != nil {
					trayUI.SetStatusBusy("IP 数据库已更新，重连后生效")
				}
			}()
		},
		OnViewLog: func() {
			exec.Command("notepad", logFile).Start()
		},
		OnToggleAuto: func(enabled bool) error {
			if err := setAutoStart(enabled); err != nil {
				log.Printf("Failed to update autostart: %v", err)
				trayUI.SetStatusError("开机自启设置失败")
				return err
			}
			cfg.AutoStart = enabled
			trayUI.SetAutoStartEnabled(enabled)
			cfg.Save()
			return nil
		},
		OnOpenDashboard: showDashboard,
		OnContactAuthor: tray.ShowContactAuthor,
		OnQuit: func() {
			log.Println("Quitting application...")
			if trayUI != nil {
				trayUI.SetStatusBusy("正在退出...")
			}
			done := make(chan struct{})
			go func() {
				vpnMon.Stop()
				disconnectSession("quit")
				cfg.Save()
				close(done)
			}()
			select {
			case <-done:
				log.Println("Quit cleanup completed")
			case <-time.After(35 * time.Second):
				log.Println("Quit cleanup timed out, forcing process exit")
			}
			os.Exit(0)
		},
	}

	// Update tray with full actions (now that all dependencies are ready)
	trayUI.SetActions(actions)
	trayUI.SetCurrentSite(connectedSite.Name)
	dashboardController = dashboard.NewController(dashboardStore, dashboard.Actions{
		OnDisconnect: func() {
			go func() {
				if err := actions.OnDisconnect(); err != nil {
					log.Printf("Dashboard disconnect failed: %v", err)
					trayUI.SetStatusError("断开 VPN 失败")
				}
			}()
		},
		OnReconnect: func() {
			go actions.OnReconnect()
		},
		OnCodexMode: func() {
			actions.OnCodexMode()
		},
		OnRestoreNormal: func() {
			actions.OnRestoreNormal()
		},
		OnToggleSplit: func(enabled bool) {
			go actions.OnToggleSplit(enabled)
		},
		OnUpdateIPDB: func() {
			actions.OnUpdateIPDB()
		},
		OnViewLog: func() {
			actions.OnViewLog()
		},
		OnToggleAutoStart: func(enabled bool) {
			go func() {
				if err := actions.OnToggleAuto(enabled); err != nil {
					log.Printf("Dashboard autostart toggle failed: %v", err)
				}
			}()
		},
		OnContactAuthor: actions.OnContactAuthor,
		OnQuit:          actions.OnQuit,
	})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			if err := dashboardController.ProcessPendingCommands(); err != nil {
				log.Printf("Dashboard command processing failed: %v", err)
			}
		}
	}()

	// Handle VPN state changes in background
	go func() {
		for change := range vpnMon.StateChanges() {
			switch change.NewState {
			case monitor.StateConnected:
				log.Println("VPN connected detected")
				applySplitRoutes(false)

			case monitor.StateCleaning:
				log.Println("VPN disconnected detected, cleaning routes...")
				cancelActiveRouteOps()
				trayUI.SetStatusBusy("Cleaning routes...")
				if usingTun() {
					if err := tunSession.Stop(); err != nil {
						log.Printf("TUN cleanup failed: %v", err)
					}
				}
				removed, _ := routeMgr.RemoveAllRoutesContext(context.Background(), cleanupProgress)
				log.Printf("Routes cleaned: %d removed", removed)
				setActiveBackend("")
				trayUI.ClearCurrentSite()
				trayUI.SetStatusIdle()
				resetRouteOps()
				vpnMon.SetState(monitor.StateIdle)
			}
		}
	}()

	// Apply split tunnel routes after tray is ready (with progress display)
	go func() {
		trayUI.WaitReady()

		if usingTun() {
			if routeMgr.HasStaleRoutes() {
				go func() {
					log.Println("TUN backend active; cleaning stale static routes in background")
					if trayUI != nil {
						trayUI.SetStatusBusy("TUN 已启用，正在后台清理旧路由...")
					}
					routeMgr.CleanupStaleRoutes()
					routeMgr.ForgetAppliedRoutes()
					if trayUI != nil {
						if cfg.SplitTunnelEnabled {
							trayUI.SetStatusTunActive()
						} else {
							trayUI.SetStatusTunFullTunnel()
						}
					}
				}()
			}
			vpnMon.Start()
			applySplitRoutes(false)
			return
		}

		if routeMgr.HasStaleRoutes() {
			targetCIDRs, err := targetBaseSplitCIDRs()
			if err != nil {
				log.Printf("Could not validate stale route record against current IP database: %v", err)
			} else if routeMgr.SavedRoutesCover(targetCIDRs) {
				if applied, count := routeMgr.AreRoutesApplied(targetCIDRs); applied {
					log.Printf("Base split routes already applied (%d saved routes), reusing existing routes", count)
					if currentBackend() == config.TrafficBackendCiscoStatic && cfg.TrafficBackend != config.TrafficBackendCiscoStatic {
						trayUI.SetStatusStaticFallback(count)
					} else {
						trayUI.SetStatusActive(count)
					}
					vpnMon.Start()
					return
				}
				log.Println("Saved route record covers target routes but active route table is incomplete")
			} else {
				log.Println("Saved route record does not cover current target routes, refreshing split routes")
			}

			trayUI.SetStatusBusy("正在清理旧分流路由...")
			routeMgr.CleanupStaleRoutes()
			routeMgr.ForgetAppliedRoutes()
		}
		vpnMon.Start()

		if !cfg.SplitTunnelEnabled {
			trayUI.SetStatusSplitDisabled()
			return
		}
		if cfg.OriginalGateway == "" {
			log.Println("Warning: no original gateway configured, skipping route application")
			return
		}

		applySplitRoutes(false)
	}()

	// Auto-update IP database check
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if time.Since(cfg.LastUpdate) > time.Duration(cfg.UpdateIntervalDays)*24*time.Hour {
				log.Println("Auto-updating IP database...")
				if _, err := db.Update(); err != nil {
					log.Printf("Auto-update failed: %v", err)
				} else {
					cfg.LastUpdate = time.Now()
					cfg.Save()
					log.Println("Auto-update completed")
					state := vpnMon.State()
					tunBackend := usingTun()
					tunActive := !tunBackend || tunSession.Active()
					if shouldRefreshAfterIPDBUpdate(cfg.SplitTunnelEnabled, state, tunBackend, tunActive) {
						resetRouteOps()
						applySplitRoutes(true)
					}
				}
			}
		}
	}()

	// Block main goroutine forever (tray is already running in its own goroutine)
	select {}
}

func setAutoStart(enabled bool) error {
	if enabled {
		return autostart.Enable()
	}
	return autostart.Disable()
}
