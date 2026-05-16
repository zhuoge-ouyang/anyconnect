package tun

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/user/anyconnect-split/internal/monitor"
)

const (
	defaultInterfaceName = "AnyConnectSplitTun"
	defaultTunAddress    = "172.19.0.1/30"
)

type Options struct {
	OpenConnectPath     string
	SingBoxPath         string
	DataDir             string
	LocalGateway        string
	LocalInterfaceIndex int
	LocalInterfaceName  string
	VPNInterfaceName    string
	InterfaceName       string
	DirectDomains       []string
}

type StartOptions struct {
	Server         string
	Username       string
	Password       string
	DirectCIDRs    []string
	ProtectedCIDRs []string
	SplitEnabled   bool
}

type Session struct {
	mu                 sync.Mutex
	opts               Options
	openConnectCmd     *exec.Cmd
	singBoxCmd         *exec.Cmd
	openConnectDone    chan error
	singBoxDone        chan error
	configPath         string
	scriptPath         string
	statePath          string
	lastDirectCIDRs    []string
	lastProtectedCIDRs []string
	lastSplit          bool
	localInterfaceName string
	vpnInterfaceName   string
}

func New(opts Options) *Session {
	if opts.InterfaceName == "" {
		opts.InterfaceName = defaultInterfaceName
	}
	return &Session{opts: opts}
}

func DetectExecutable(configured, exeName string) string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		if fileExists(configured) {
			return configured
		}
		return configured
	}
	if path, err := exec.LookPath(exeName); err == nil {
		return path
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	base := filepath.Dir(exe)
	for _, candidate := range []string{
		filepath.Join(base, exeName),
		filepath.Join(base, "tools", exeName),
		filepath.Join(base, "bin", exeName),
		filepath.Join(base, "openconnect", exeName),
	} {
		if fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

func (s *Session) ResolveTools() {
	s.opts.OpenConnectPath = DetectExecutable(s.opts.OpenConnectPath, "openconnect.exe")
	s.opts.SingBoxPath = DetectExecutable(s.opts.SingBoxPath, "sing-box.exe")
}

func (s *Session) ToolsAvailable() error {
	s.ResolveTools()
	missing := make([]string, 0, 2)
	if !fileExists(s.opts.OpenConnectPath) {
		missing = append(missing, "openconnect.exe")
	}
	if !fileExists(s.opts.SingBoxPath) {
		missing = append(missing, "sing-box.exe")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing TUN tool(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

func (s *Session) Start(ctx context.Context, start StartOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.stopLocked(); err != nil {
		log.Printf("Warning: failed to stop previous TUN session: %v", err)
	}
	if err := s.ToolsAvailable(); err != nil {
		return err
	}
	if strings.TrimSpace(start.Server) == "" || strings.TrimSpace(start.Username) == "" {
		return fmt.Errorf("missing OpenConnect server or username")
	}
	if strings.TrimSpace(start.Password) == "" {
		return fmt.Errorf("missing OpenConnect password")
	}
	if s.opts.DataDir == "" {
		return fmt.Errorf("missing TUN data directory")
	}
	if err := os.MkdirAll(s.opts.DataDir, 0755); err != nil {
		return err
	}
	localName, err := s.resolveLocalInterfaceName()
	if err != nil {
		return err
	}
	s.scriptPath = filepath.Join(s.opts.DataDir, "openconnect-lite.js")
	s.statePath = filepath.Join(s.opts.DataDir, "openconnect-state.txt")
	if err := s.writeOpenConnectScript(); err != nil {
		return err
	}
	_ = os.Remove(s.statePath)

	openConnectCmd, done, err := s.startOpenConnect(start)
	if err != nil {
		return err
	}
	s.openConnectCmd = openConnectCmd
	s.openConnectDone = done

	vpnName, err := s.waitForVPNInterface(ctx, localName, 45*time.Second)
	if err != nil {
		_ = s.stopLocked()
		return err
	}
	s.localInterfaceName = localName
	s.vpnInterfaceName = vpnName
	if err := s.startSingBoxLocked(start.DirectCIDRs, start.ProtectedCIDRs, start.SplitEnabled); err != nil {
		_ = s.stopLocked()
		return err
	}

	s.lastDirectCIDRs = append([]string(nil), start.DirectCIDRs...)
	s.lastProtectedCIDRs = append([]string(nil), start.ProtectedCIDRs...)
	s.lastSplit = start.SplitEnabled
	log.Printf("OpenConnect TUN backend started: tun=%s local_interface=%s vpn_interface=%s split=%v direct_cidrs=%d protected_cidrs=%d",
		s.opts.InterfaceName, s.localInterfaceName, s.vpnInterfaceName, start.SplitEnabled, len(start.DirectCIDRs), len(start.ProtectedCIDRs))
	return nil
}

func (s *Session) Refresh(ctx context.Context, directCIDRs []string, protectedCIDRs []string, splitEnabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.openConnectAliveLocked() {
		return fmt.Errorf("OpenConnect TUN session is not active")
	}
	if err := s.stopSingBoxLocked(); err != nil {
		log.Printf("Warning: failed to stop sing-box before refresh: %v", err)
	}
	if err := s.startSingBoxLocked(directCIDRs, protectedCIDRs, splitEnabled); err != nil {
		return err
	}
	s.lastDirectCIDRs = append([]string(nil), directCIDRs...)
	s.lastProtectedCIDRs = append([]string(nil), protectedCIDRs...)
	s.lastSplit = splitEnabled
	log.Printf("OpenConnect TUN routes refreshed: split=%v direct_cidrs=%d protected_cidrs=%d",
		splitEnabled, len(directCIDRs), len(protectedCIDRs))
	return nil
}

func (s *Session) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopLocked()
}

func (s *Session) Presence() monitor.VPNPresence {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openConnectAliveLocked() && s.singBoxAliveLocked() {
		return monitor.PresenceConnected
	}
	if s.openConnectCmd != nil || s.singBoxCmd != nil {
		return monitor.PresenceUnknown
	}
	return monitor.PresenceDisconnected
}

func (s *Session) Active() bool {
	return s.Presence() == monitor.PresenceConnected
}

func (s *Session) startOpenConnect(start StartOptions) (*exec.Cmd, chan error, error) {
	args := OpenConnectArgs(start.Server, start.Username, s.scriptPath)
	log.Printf("Starting OpenConnect: %s %s", s.opts.OpenConnectPath, RedactArgs(args))
	cmd := exec.Command(s.opts.OpenConnectPath, args...)
	cmd.Env = append(os.Environ(),
		"ANYCONNECT_SPLIT_LOCAL_GATEWAY="+s.opts.LocalGateway,
		"ANYCONNECT_SPLIT_STATE_FILE="+s.statePath,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	go logPipe("openconnect", stdout)
	go logPipe("openconnect", stderr)

	if _, err := stdin.Write([]byte(start.Password + "\n")); err != nil {
		_ = killProcessTree(cmd)
		return nil, nil, err
	}
	_ = stdin.Close()

	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			log.Printf("OpenConnect exited: %v", err)
		} else {
			log.Printf("OpenConnect exited")
		}
		done <- err
		close(done)
	}()
	return cmd, done, nil
}

func (s *Session) startSingBoxLocked(directCIDRs []string, protectedCIDRs []string, splitEnabled bool) error {
	configBytes, err := BuildSingBoxConfig(ConfigOptions{
		InterfaceName:  s.opts.InterfaceName,
		TunAddress:     defaultTunAddress,
		LocalInterface: s.localInterfaceName,
		VPNInterface:   s.vpnInterfaceName,
		DirectCIDRs:    directCIDRs,
		ProtectedCIDRs: protectedCIDRs,
		DirectDomains:  s.opts.DirectDomains,
		SplitEnabled:   splitEnabled,
	})
	if err != nil {
		return err
	}
	configPath := filepath.Join(s.opts.DataDir, "sing-box-tun.json")
	if err := os.WriteFile(configPath, configBytes, 0644); err != nil {
		return err
	}
	cmd := exec.Command(s.opts.SingBoxPath, "run", "-c", configPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go logPipe("sing-box", stdout)
	go logPipe("sing-box", stderr)

	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			log.Printf("sing-box exited: %v", err)
		} else {
			log.Printf("sing-box exited")
		}
		done <- err
		close(done)
	}()
	s.singBoxCmd = cmd
	s.singBoxDone = done
	s.configPath = configPath
	if err := s.waitForSingBox(12 * time.Second); err != nil {
		_ = s.stopSingBoxLocked()
		return err
	}
	return nil
}

func (s *Session) waitForVPNInterface(ctx context.Context, localName string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !s.openConnectAliveLocked() {
			return "", fmt.Errorf("OpenConnect exited before VPN interface became available")
		}
		if name, err := s.readStateInterfaceName(); err == nil {
			return name, nil
		} else {
			lastErr = err
		}
		name, err := s.detectVPNInterfaceName(localName)
		if err == nil {
			return name, nil
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	return "", fmt.Errorf("OpenConnect VPN interface not available: %w", lastErr)
}

func (s *Session) waitForSingBox(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var sawProcess bool
	for time.Now().Before(deadline) {
		if !s.singBoxAliveLocked() {
			return fmt.Errorf("sing-box exited before TUN became ready")
		}
		sawProcess = true
		if iface, err := net.InterfaceByName(s.opts.InterfaceName); err == nil && iface != nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	if sawProcess {
		log.Printf("Warning: sing-box is running but TUN interface %s was not visible yet", s.opts.InterfaceName)
		return nil
	}
	return fmt.Errorf("sing-box did not start")
}

func (s *Session) stopLocked() error {
	var errs []string
	if err := s.stopSingBoxLocked(); err != nil {
		errs = append(errs, err.Error())
	}
	if s.openConnectCmd != nil {
		if err := killProcessTree(s.openConnectCmd); err != nil {
			errs = append(errs, err.Error())
		}
	}
	s.openConnectCmd = nil
	s.openConnectDone = nil
	s.lastDirectCIDRs = nil
	s.lastProtectedCIDRs = nil
	s.lastSplit = false
	s.localInterfaceName = ""
	s.vpnInterfaceName = ""
	if s.statePath != "" {
		_ = os.Remove(s.statePath)
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (s *Session) stopSingBoxLocked() error {
	if s.singBoxCmd == nil {
		return nil
	}
	err := killProcessTree(s.singBoxCmd)
	s.singBoxCmd = nil
	s.singBoxDone = nil
	return err
}

func (s *Session) openConnectAliveLocked() bool {
	return processAlive(s.openConnectCmd, s.openConnectDone)
}

func (s *Session) singBoxAliveLocked() bool {
	return processAlive(s.singBoxCmd, s.singBoxDone)
}

func processAlive(cmd *exec.Cmd, done <-chan error) bool {
	if cmd == nil {
		return false
	}
	if done == nil {
		return cmd.ProcessState == nil
	}
	select {
	case <-done:
		return false
	default:
		return true
	}
}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	kill := exec.Command("taskkill", "/T", "/F", "/PID", pid)
	kill.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	output, err := kill.CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(output)), "not found") {
		return fmt.Errorf("taskkill %s failed: %s: %w", pid, strings.TrimSpace(string(output)), err)
	}
	return nil
}

func OpenConnectArgs(server, username, scriptPath string) []string {
	return []string{
		"--protocol=anyconnect",
		"--user=" + username,
		"--passwd-on-stdin",
		"--script", scriptPath,
		server,
	}
}

func RedactArgs(args []string) string {
	return strings.Join(args, " ")
}

func (s *Session) resolveLocalInterfaceName() (string, error) {
	if strings.TrimSpace(s.opts.LocalInterfaceName) != "" {
		return strings.TrimSpace(s.opts.LocalInterfaceName), nil
	}
	if s.opts.LocalInterfaceIndex > 0 {
		iface, err := net.InterfaceByIndex(s.opts.LocalInterfaceIndex)
		if err == nil && iface != nil && strings.TrimSpace(iface.Name) != "" {
			return iface.Name, nil
		}
		return "", fmt.Errorf("local interface index %d not found: %w", s.opts.LocalInterfaceIndex, err)
	}
	return "", fmt.Errorf("missing local interface for TUN direct outbound")
}

func (s *Session) detectVPNInterfaceName(localName string) (string, error) {
	if strings.TrimSpace(s.opts.VPNInterfaceName) != "" {
		return strings.TrimSpace(s.opts.VPNInterfaceName), nil
	}
	script := `
$localName = $env:ANYCONNECT_SPLIT_LOCAL_INTERFACE
$adapters = Get-NetAdapter -IncludeHidden -ErrorAction SilentlyContinue |
  Where-Object {
    $_.Status -eq 'Up' -and
    $_.Name -ne $localName -and
    ($_.Name -match 'OpenConnect|Wintun|TAP' -or $_.InterfaceDescription -match 'OpenConnect|Wintun|TAP') -and
    ($_.Name -notmatch 'Cisco|AnyConnect' -and $_.InterfaceDescription -notmatch 'Cisco|AnyConnect')
  } |
  Sort-Object InterfaceMetric, ifIndex
$adapters | Select-Object -First 1 -ExpandProperty Name
`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.Env = append(os.Environ(), "ANYCONNECT_SPLIT_LOCAL_INTERFACE="+localName)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(string(output))
	if name == "" {
		return "", fmt.Errorf("no OpenConnect adapter found")
	}
	return name, nil
}

func (s *Session) readStateInterfaceName() (string, error) {
	if s.statePath == "" {
		return "", fmt.Errorf("missing OpenConnect state path")
	}
	data, err := os.ReadFile(s.statePath)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(string(data))
	if name == "" {
		return "", fmt.Errorf("OpenConnect state file is empty")
	}
	return name, nil
}

func (s *Session) writeOpenConnectScript() error {
	if s.scriptPath == "" {
		return fmt.Errorf("missing OpenConnect script path")
	}
	script := `var ws = WScript.CreateObject("WScript.Shell");
var fs = WScript.CreateObject("Scripting.FileSystemObject");
var env = ws.Environment("Process");

function e(name) {
    return env(name) || "";
}

function q(value) {
    return "\"" + String(value).replace(/"/g, "\\\"") + "\"";
}

function run(cmd) {
    try {
        ws.Run("%comspec% /C " + cmd + " >NUL 2>&1", 0, true);
    } catch (ignored) {
    }
}

function writeState(value) {
    var path = e("ANYCONNECT_SPLIT_STATE_FILE");
    if (!path) return;
    try {
        var f = fs.OpenTextFile(path, 2, true);
        f.WriteLine(value);
        f.Close();
    } catch (ignored) {
    }
}

function deleteState() {
    var path = e("ANYCONNECT_SPLIT_STATE_FILE");
    if (!path) return;
    try {
        if (fs.FileExists(path)) fs.DeleteFile(path, true);
    } catch (ignored) {
    }
}

var reason = e("reason").toLowerCase();
if (reason === "connect") {
    var tunIdx = e("TUNIDX");
    var tunDev = e("TUNDEV");
    var ip4 = e("INTERNAL_IP4_ADDRESS");
    var mask4 = e("INTERNAL_IP4_NETMASK") || "255.255.255.255";
    var mtu = e("INTERNAL_IP4_MTU");
    var vpnGateway = e("VPNGATEWAY");
    var localGateway = e("ANYCONNECT_SPLIT_LOCAL_GATEWAY");

    writeState(tunDev);
    if (tunIdx && mtu) run("netsh interface ipv4 set subinterface " + tunIdx + " mtu=" + mtu + " store=active");
    if (tunIdx) run("netsh interface ip set interface " + tunIdx + " metric=1 store=active");
    if (tunIdx && ip4) run("netsh interface ip set address " + tunIdx + " static " + ip4 + " " + mask4 + " " + ip4 + " gwmetric=1 store=active");
    if (vpnGateway && localGateway) run("route add " + vpnGateway + " mask 255.255.255.255 " + localGateway + " metric 1");
} else if (reason === "disconnect") {
    var vpnGateway2 = e("VPNGATEWAY");
    deleteState();
    if (vpnGateway2) run("route delete " + vpnGateway2);
}
WScript.Quit(0);
`
	return os.WriteFile(s.scriptPath, []byte(script), 0644)
}

func QuoteScriptPath(path string) string {
	if strings.ContainsAny(path, " \t") && !strings.HasPrefix(path, `"`) {
		return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
	}
	return path
}

func logPipe(prefix string, pipe interface{ Read([]byte) (int, error) }) {
	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			log.Printf("%s: %s", prefix, line)
		}
	}
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

type ConfigOptions struct {
	InterfaceName  string
	TunAddress     string
	LocalInterface string
	VPNInterface   string
	DirectCIDRs    []string
	ProtectedCIDRs []string
	DirectDomains  []string
	SplitEnabled   bool
}

func BuildSingBoxConfig(opts ConfigOptions) ([]byte, error) {
	if opts.InterfaceName == "" {
		opts.InterfaceName = defaultInterfaceName
	}
	if opts.TunAddress == "" {
		opts.TunAddress = defaultTunAddress
	}
	if strings.TrimSpace(opts.LocalInterface) == "" || strings.TrimSpace(opts.VPNInterface) == "" {
		return nil, fmt.Errorf("missing local or VPN interface")
	}

	rules := []map[string]any{
		{
			"protocol": []string{"dns"},
			"action":   "hijack-dns",
		},
		{
			"ip_is_private": true,
			"outbound":      "direct-local",
		},
	}
	if len(opts.ProtectedCIDRs) > 0 {
		rules = append(rules, map[string]any{
			"ip_cidr":  uniqueStrings(opts.ProtectedCIDRs),
			"outbound": "direct-local",
		})
	}
	if opts.SplitEnabled {
		if len(opts.DirectCIDRs) > 0 {
			rules = append(rules, map[string]any{
				"ip_cidr":  uniqueStrings(opts.DirectCIDRs),
				"outbound": "direct-local",
			})
		}
		if len(opts.DirectDomains) > 0 {
			rules = append(rules, map[string]any{
				"domain_suffix": uniqueStrings(opts.DirectDomains),
				"outbound":      "direct-local",
			})
		}
	}

	// Build DNS configuration
	dnsServers := []map[string]any{
		{
			"tag":     "dns-vpn",
			"address": "tls://8.8.8.8",
			"detour":  "vpn-direct",
		},
		{
			"tag":     "dns-local",
			"address": "114.114.114.114",
			"detour":  "direct-local",
		},
	}
	dnsConfig := map[string]any{
		"servers":  dnsServers,
		"final":    "dns-vpn",
		"strategy": "prefer_ipv4",
	}
	// In split mode, route domestic domains to local DNS
	if opts.SplitEnabled && len(opts.DirectDomains) > 0 {
		suffixes := make([]string, 0, len(opts.DirectDomains))
		for _, d := range uniqueStrings(opts.DirectDomains) {
			d = strings.TrimPrefix(d, ".")
			if d != "" {
				suffixes = append(suffixes, d)
			}
		}
		if len(suffixes) > 0 {
			dnsConfig["rules"] = []map[string]any{
				{
					"domain_suffix": suffixes,
					"server":        "dns-local",
				},
			}
		}
	}

	config := map[string]any{
		"log": map[string]any{
			"level": "info",
		},
		"dns": dnsConfig,
		"inbounds": []map[string]any{
			{
				"type":                       "tun",
				"tag":                        "tun-in",
				"interface_name":             opts.InterfaceName,
				"address":                    []string{opts.TunAddress},
				"auto_route":                 true,
				"strict_route":               true,
				"stack":                      "mixed",
				"sniff":                      true,
				"sniff_override_destination": true,
			},
		},
		"outbounds": []map[string]any{
			{
				"type":           "direct",
				"tag":            "direct-local",
				"bind_interface": opts.LocalInterface,
			},
			{
				"type":           "direct",
				"tag":            "vpn-direct",
				"bind_interface": opts.VPNInterface,
			},
		},
		"route": map[string]any{
			"auto_detect_interface": true,
			"rules":                 rules,
			"final":                 "vpn-direct",
		},
	}
	return json.MarshalIndent(config, "", "  ")
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
