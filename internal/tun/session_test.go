package tun

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/user/anyconnect-split/internal/monitor"
)

func TestOpenConnectArgsDoNotContainPassword(t *testing.T) {
	args := OpenConnectArgs(
		"https://vpn.example.com:10000",
		"alice",
		`D:\app data\openconnect-lite.js`,
	)
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "secret") {
		t.Fatal("OpenConnectArgs() should not include password")
	}
	if !strings.Contains(joined, "--passwd-on-stdin") {
		t.Fatalf("OpenConnectArgs() = %q, want --passwd-on-stdin", joined)
	}
	if strings.Contains(joined, "--script-tun") {
		t.Fatalf("OpenConnectArgs() = %q, should use native OpenConnect adapter mode", joined)
	}
	if !strings.Contains(joined, `D:\app data\openconnect-lite.js`) {
		t.Fatalf("OpenConnectArgs() script = %q, want generated lightweight script", joined)
	}
}

func TestBuildSingBoxConfigSplitRules(t *testing.T) {
	data, err := BuildSingBoxConfig(ConfigOptions{
		InterfaceName:  "AnyConnectSplitTun",
		LocalInterface: "Wi-Fi",
		VPNInterface:   "OpenConnect",
		DirectCIDRs:    []string{"1.0.1.0/24", "1.0.1.0/24", "114.16.0.0/12"},
		DirectDomains:  []string{"baidu.com", "baidu.com"},
		SplitEnabled:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	routeCfg := cfg["route"].(map[string]any)
	if routeCfg["final"] != "vpn-direct" {
		t.Fatalf("route.final = %v, want vpn-direct", routeCfg["final"])
	}
	rules := routeCfg["rules"].([]any)
	if len(rules) != 5 {
		t.Fatalf("len(rules) = %d, want hijack-dns, private, cidr, domain, udp 443 reject rules", len(rules))
	}
	// First rule must be hijack-dns action
	dnsRule := rules[0].(map[string]any)
	if dnsRule["action"] != "hijack-dns" {
		t.Fatalf("rules[0].action = %v, want hijack-dns", dnsRule["action"])
	}
	cidrRule := rules[2].(map[string]any)
	cidrs := cidrRule["ip_cidr"].([]any)
	if len(cidrs) != 2 {
		t.Fatalf("len(ip_cidr) = %d, want deduped 2", len(cidrs))
	}
	domainRule := rules[3].(map[string]any)
	domains := domainRule["domain_suffix"].([]any)
	if len(domains) != 1 {
		t.Fatalf("len(domain_suffix) = %d, want deduped 1", len(domains))
	}
	udp443Rule := rules[4].(map[string]any)
	if udp443Rule["network"] != "udp" || udp443Rule["port"] != float64(443) || udp443Rule["action"] != "reject" {
		t.Fatalf("rules[4] = %v, want UDP/443 reject rule", udp443Rule)
	}
}

func TestBuildSingBoxConfigDefaultsToWarnLogLevel(t *testing.T) {
	data, err := BuildSingBoxConfig(ConfigOptions{
		LocalInterface: "Wi-Fi",
		VPNInterface:   "OpenConnect",
		SplitEnabled:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	logCfg := cfg["log"].(map[string]any)
	if logCfg["level"] != "warn" {
		t.Fatalf("log.level = %v, want warn", logCfg["level"])
	}
}

func TestBuildSingBoxConfigHonorsSupportedLogLevel(t *testing.T) {
	data, err := BuildSingBoxConfig(ConfigOptions{
		LocalInterface: "Wi-Fi",
		VPNInterface:   "OpenConnect",
		LogLevel:       "error",
		SplitEnabled:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	logCfg := cfg["log"].(map[string]any)
	if logCfg["level"] != "error" {
		t.Fatalf("log.level = %v, want error", logCfg["level"])
	}
}

func TestBuildSingBoxConfigDNSSplit(t *testing.T) {
	data, err := BuildSingBoxConfig(ConfigOptions{
		LocalInterface: "Wi-Fi",
		VPNInterface:   "OpenConnect",
		DirectDomains:  []string{".cn", ".com.cn", "baidu.com"},
		SplitEnabled:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	dnsCfg, ok := cfg["dns"].(map[string]any)
	if !ok {
		t.Fatal("dns section missing")
	}
	if dnsCfg["final"] != "dns-vpn" {
		t.Fatalf("dns.final = %v, want dns-vpn", dnsCfg["final"])
	}
	if dnsCfg["strategy"] != "prefer_ipv4" {
		t.Fatalf("dns.strategy = %v, want prefer_ipv4", dnsCfg["strategy"])
	}
	servers := dnsCfg["servers"].([]any)
	if len(servers) != 2 {
		t.Fatalf("len(dns.servers) = %d, want 2", len(servers))
	}
	vpnSrv := servers[0].(map[string]any)
	if vpnSrv["tag"] != "dns-vpn" || vpnSrv["address"] != "tls://8.8.8.8" || vpnSrv["detour"] != "vpn-direct" {
		t.Fatalf("dns-vpn server = %v, unexpected", vpnSrv)
	}
	localSrv := servers[1].(map[string]any)
	if localSrv["tag"] != "dns-local" || localSrv["address"] != "114.114.114.114" || localSrv["detour"] != "direct-local" {
		t.Fatalf("dns-local server = %v, unexpected", localSrv)
	}
	// Check DNS rules strip leading dots
	dnsRules := dnsCfg["rules"].([]any)
	if len(dnsRules) != 1 {
		t.Fatalf("len(dns.rules) = %d, want 1", len(dnsRules))
	}
	rule0 := dnsRules[0].(map[string]any)
	suffixes := rule0["domain_suffix"].([]any)
	for _, s := range suffixes {
		if strings.HasPrefix(s.(string), ".") {
			t.Fatalf("dns rule domain_suffix %q should not have leading dot", s)
		}
	}
	if rule0["server"] != "dns-local" {
		t.Fatalf("dns rule server = %v, want dns-local", rule0["server"])
	}
}

func TestBuildSingBoxConfigDNSFullTunnel(t *testing.T) {
	data, err := BuildSingBoxConfig(ConfigOptions{
		LocalInterface: "Wi-Fi",
		VPNInterface:   "OpenConnect",
		DirectDomains:  []string{"cn", "com.cn"},
		SplitEnabled:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	dnsCfg := cfg["dns"].(map[string]any)
	if dnsCfg["final"] != "dns-vpn" {
		t.Fatalf("dns.final = %v, want dns-vpn", dnsCfg["final"])
	}
	// Full tunnel mode should have no DNS rules (all DNS via VPN)
	if _, hasRules := dnsCfg["rules"]; hasRules {
		t.Fatal("full tunnel mode should not have dns rules")
	}
}

func TestBuildSingBoxConfigFullTunnelOmitsCNRules(t *testing.T) {
	data, err := BuildSingBoxConfig(ConfigOptions{
		LocalInterface: "Wi-Fi",
		VPNInterface:   "OpenConnect",
		DirectCIDRs:    []string{"1.0.1.0/24"},
		SplitEnabled:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	routeCfg := cfg["route"].(map[string]any)
	rules := routeCfg["rules"].([]any)
	if len(rules) != 3 {
		t.Fatalf("len(rules) = %d, want hijack-dns + private direct + udp 443 reject rule in full tunnel mode", len(rules))
	}
	// First rule must be hijack-dns action
	dnsRule := rules[0].(map[string]any)
	if dnsRule["action"] != "hijack-dns" {
		t.Fatalf("rules[0].action = %v, want hijack-dns", dnsRule["action"])
	}
	// Verify DNS section exists even in full tunnel mode
	if _, ok := cfg["dns"]; !ok {
		t.Fatal("dns section missing in full tunnel config")
	}
	udp443Rule := rules[2].(map[string]any)
	if udp443Rule["network"] != "udp" || udp443Rule["port"] != float64(443) || udp443Rule["action"] != "reject" {
		t.Fatalf("rules[2] = %v, want UDP/443 reject rule", udp443Rule)
	}
}

func TestBuildSingBoxConfigDomesticDirectFullTunnelUsesVPN(t *testing.T) {
	data, err := BuildSingBoxConfig(ConfigOptions{
		LocalInterface: "Wi-Fi",
		VPNInterface:   "OpenConnect",
		ForeignDomains: []string{"chatgpt.com"},
		SplitMode:      "domestic_direct",
		SplitEnabled:   false,
	})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	routeCfg := cfg["route"].(map[string]any)
	if routeCfg["final"] != "vpn-direct" {
		t.Fatalf("route.final = %v, want vpn-direct when split tunneling is disabled", routeCfg["final"])
	}
	dnsCfg := cfg["dns"].(map[string]any)
	if dnsCfg["final"] != "dns-vpn" {
		t.Fatalf("dns.final = %v, want dns-vpn when split tunneling is disabled", dnsCfg["final"])
	}
	if _, hasRules := dnsCfg["rules"]; hasRules {
		t.Fatal("full tunnel mode should not retain split DNS rules")
	}
}

func TestBuildSingBoxConfigDomesticDirectDefaultsToDirect(t *testing.T) {
	data, err := BuildSingBoxConfig(ConfigOptions{
		LocalInterface: "Wi-Fi",
		VPNInterface:   "OpenConnect",
		ForeignCIDRs:   []string{"1.2.3.0/24", "1.2.3.0/24"},
		ForeignDomains: []string{"openai.com", "openai.com"},
		SplitMode:      "domestic_direct",
		SplitEnabled:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	routeCfg := cfg["route"].(map[string]any)
	// domestic_direct: default traffic is direct, only foreign whitelist via VPN.
	if routeCfg["final"] != "direct-local" {
		t.Fatalf("route.final = %v, want direct-local", routeCfg["final"])
	}
	rules := routeCfg["rules"].([]any)
	if len(rules) != 5 {
		t.Fatalf("len(rules) = %d, want hijack-dns + private + foreign-cidr + foreign-domain + udp 443 reject", len(rules))
	}
	// Foreign CIDR rule must route to vpn-direct.
	cidrRule := rules[2].(map[string]any)
	if cidrRule["outbound"] != "vpn-direct" {
		t.Fatalf("foreign cidr outbound = %v, want vpn-direct", cidrRule["outbound"])
	}
	cidrs := cidrRule["ip_cidr"].([]any)
	if len(cidrs) != 1 {
		t.Fatalf("len(ip_cidr) = %d, want deduped 1", len(cidrs))
	}
	// Foreign domain rule must route to vpn-direct.
	domainRule := rules[3].(map[string]any)
	if domainRule["outbound"] != "vpn-direct" {
		t.Fatalf("foreign domain outbound = %v, want vpn-direct", domainRule["outbound"])
	}
	// DNS final should be dns-local (default direct), foreign domains via dns-vpn.
	dnsCfg := cfg["dns"].(map[string]any)
	if dnsCfg["final"] != "dns-local" {
		t.Fatalf("dns.final = %v, want dns-local", dnsCfg["final"])
	}
	dnsRules := dnsCfg["rules"].([]any)
	rule0 := dnsRules[0].(map[string]any)
	if rule0["server"] != "dns-vpn" {
		t.Fatalf("dns rule server = %v, want dns-vpn for foreign domains", rule0["server"])
	}
}

func TestSessionSetSplitModeValidatesAndUpdatesOptions(t *testing.T) {
	s := New(Options{SplitMode: "domestic_direct"})

	if !s.SetSplitMode(" FOREIGN_DIRECT ") {
		t.Fatal("SetSplitMode rejected a supported mode")
	}
	if s.opts.SplitMode != "foreign_direct" {
		t.Fatalf("session split mode = %q, want foreign_direct", s.opts.SplitMode)
	}
	if s.SetSplitMode("invalid") {
		t.Fatal("SetSplitMode accepted an unsupported mode")
	}
	if s.opts.SplitMode != "foreign_direct" {
		t.Fatalf("invalid update changed session split mode to %q", s.opts.SplitMode)
	}
}

func TestDetectExecutableUsesConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openconnect.exe")
	if err := os.WriteFile(path, []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := DetectExecutable(path, "missing.exe"); got != path {
		t.Fatalf("DetectExecutable() = %q, want configured path", got)
	}
}

func TestPresenceReportsDisconnectedWhenChildProcessesExited(t *testing.T) {
	openConnectDone := make(chan error)
	close(openConnectDone)
	singBoxDone := make(chan error)
	close(singBoxDone)
	session := &Session{
		openConnectCmd:  &exec.Cmd{},
		openConnectDone: openConnectDone,
		singBoxCmd:      &exec.Cmd{},
		singBoxDone:     singBoxDone,
	}

	if got := session.Presence(); got != monitor.PresenceDisconnected {
		t.Fatalf("Presence() = %s, want %s", got, monitor.PresenceDisconnected)
	}
}

func TestResolveLocalInterfaceRefreshesStalePersistedIndex(t *testing.T) {
	route, err := monitor.GetDefaultRoute()
	if err != nil {
		t.Skipf("default route unavailable in test environment: %v", err)
	}
	iface, err := net.InterfaceByIndex(route.InterfaceIndex)
	if err != nil || iface == nil {
		t.Skipf("default route interface unavailable in test environment: %v", err)
	}

	session := &Session{opts: Options{
		LocalGateway:        "192.0.2.1",
		LocalInterfaceIndex: 2147483647,
	}}

	got, err := session.resolveLocalInterfaceName()
	if err != nil {
		t.Fatalf("resolveLocalInterfaceName() returned error: %v", err)
	}
	if got != iface.Name {
		t.Fatalf("resolveLocalInterfaceName() = %q, want current default interface %q", got, iface.Name)
	}
	if session.opts.LocalGateway != route.Gateway {
		t.Fatalf("session local gateway = %q, want refreshed gateway %q", session.opts.LocalGateway, route.Gateway)
	}
	if session.opts.LocalInterfaceIndex != route.InterfaceIndex {
		t.Fatalf("session interface index = %d, want refreshed index %d", session.opts.LocalInterfaceIndex, route.InterfaceIndex)
	}
}

func TestPresenceReportsDisconnectedWhenOneBackendDies(t *testing.T) {
	singBoxDone := make(chan error)
	close(singBoxDone)
	session := &Session{
		openConnectCmd: &exec.Cmd{},
		singBoxCmd:     &exec.Cmd{},
		singBoxDone:    singBoxDone,
	}

	if got := session.Presence(); got != monitor.PresenceDisconnected {
		t.Fatalf("Presence() = %s, want %s for a partial TUN backend", got, monitor.PresenceDisconnected)
	}
}

func TestWaitForVPNInterfaceReturnsAuthFailureWhenOpenConnectRejectsLogin(t *testing.T) {
	openConnectDone := make(chan error)
	close(openConnectDone)
	session := &Session{
		openConnectCmd:  &exec.Cmd{},
		openConnectDone: openConnectDone,
	}
	session.openConnectAuthFailed.Store(true)

	_, err := session.waitForVPNInterface(context.Background(), "Wi-Fi", time.Second)
	if !errors.Is(err, ErrOpenConnectAuthentication) {
		t.Fatalf("waitForVPNInterface() error = %v, want ErrOpenConnectAuthentication", err)
	}
}

func TestWaitForSingBoxDoesNotAcceptStaleInterfaceWhenProcessExits(t *testing.T) {
	ifaceName := firstInterfaceName(t)
	cmd := exec.Command("cmd", "/C", "exit /B 1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start short-lived command: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()

	session := &Session{
		opts:        Options{InterfaceName: ifaceName},
		singBoxCmd:  cmd,
		singBoxDone: done,
	}

	err := session.waitForSingBox(3 * time.Second)
	if err == nil {
		t.Fatal("waitForSingBox() accepted a stale interface while the process exited")
	}
	if !strings.Contains(err.Error(), "exited") {
		t.Fatalf("waitForSingBox() error = %v, want process exit error", err)
	}
}

func TestStaleBackendProcessCleanupScriptTargetsOnlySessionArtifacts(t *testing.T) {
	s := &Session{
		opts: Options{
			DataDir:       `D:\project\anyconnect\bin\data`,
			InterfaceName: "AnyConnectSplitTun",
		},
		scriptPath: `D:\project\anyconnect\bin\data\openconnect-lite.js`,
		configPath: `D:\project\anyconnect\bin\data\sing-box-tun.json`,
	}

	script := s.staleBackendProcessCleanupScript()
	for _, want := range []string{
		`sing-box-tun.json`,
		`openconnect-lite.js`,
		`AnyConnectSplitTun`,
		`Stop-Process`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("cleanup script missing %q:\n%s", want, script)
		}
	}
	if !strings.Contains(script, "CommandLine") {
		t.Fatalf("cleanup script must filter by command line, got:\n%s", script)
	}
}

func firstInterfaceName(t *testing.T) string {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("list interfaces: %v", err)
	}
	for _, iface := range ifaces {
		if strings.TrimSpace(iface.Name) != "" {
			return iface.Name
		}
	}
	t.Fatal("no network interface available for test")
	return ""
}
