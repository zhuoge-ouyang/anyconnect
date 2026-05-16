package tun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if len(rules) != 4 {
		t.Fatalf("len(rules) = %d, want hijack-dns, private, cidr, domain rules", len(rules))
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
	if len(rules) != 2 {
		t.Fatalf("len(rules) = %d, want hijack-dns + private direct rule in full tunnel mode", len(rules))
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
