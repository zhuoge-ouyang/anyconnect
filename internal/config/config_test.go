package config

import (
	"strings"
	"testing"
)

func TestDefaultConfigIncludesVPNSites(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.VPNSites) == 0 {
		t.Fatal("DefaultConfig() has no VPN sites")
	}
	if cfg.PreferredSite != DefaultGlobalPreferredSite {
		t.Fatalf("DefaultConfig().PreferredSite = %q, want global site", cfg.PreferredSite)
	}
	if cfg.IPv6SplitEnabled {
		t.Fatal("DefaultConfig().IPv6SplitEnabled = true, want false")
	}
	if cfg.CodexPreferredSite != DefaultGlobalPreferredSite {
		t.Fatalf("DefaultConfig().CodexPreferredSite = %q, want Australia site", cfg.CodexPreferredSite)
	}
	if cfg.TrafficBackend != TrafficBackendAuto {
		t.Fatalf("DefaultConfig().TrafficBackend = %q, want auto", cfg.TrafficBackend)
	}
	if cfg.LogLevel != "warn" {
		t.Fatalf("DefaultConfig().LogLevel = %q, want warn", cfg.LogLevel)
	}
	if len(cfg.CodexCandidateSites) == 0 {
		t.Fatal("DefaultConfig().CodexCandidateSites is empty")
	}
	if strings.Contains(cfg.CodexCandidateSites[0], "国内") {
		t.Fatalf("DefaultConfig().CodexCandidateSites[0] = %q, want global site first", cfg.CodexCandidateSites[0])
	}
}

func TestNormalizeRepairsInvalidPreferredSite(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PreferredSite = string([]byte{0xb9, 0xfa, 0xc4, 0xda})
	cfg.normalize()
	if cfg.PreferredSite != DefaultGlobalPreferredSite {
		t.Fatalf("normalize() PreferredSite = %q, want global site", cfg.PreferredSite)
	}
}

func TestNormalizePreservesDomesticPreferredSite(t *testing.T) {
	cfg := DefaultConfig()
	cfg.PreferredSite = "03.国内专线-深圳节点"
	cfg.normalize()
	if cfg.PreferredSite != "03.国内专线-深圳节点" {
		t.Fatalf("normalize() PreferredSite = %q, want Shenzhen site", cfg.PreferredSite)
	}
}

func TestDefaultDomesticDomainsIncludeRequestedWhitelist(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.DomesticDomains) == 0 {
		t.Fatal("DefaultConfig().DomesticDomains is empty")
	}
	set := make(map[string]struct{}, len(cfg.DomesticDomains))
	for _, domain := range cfg.DomesticDomains {
		set[domain] = struct{}{}
	}
	want := []string{
		"weixin.qq.com",
		"tim.qq.com",
		"bilibili.com",
		"taobao.com",
		"tmall.com",
		"jd.com",
		"goofish.com",
		"xiaohongshu.com",
		"douyin.com",
		"pinduoduo.com",
		"icbc.com.cn",
		"ccb.com",
		"cmbchina.com",
		"5211game.com",
		"mihoyo.com",
	}
	for _, domain := range want {
		if _, ok := set[domain]; !ok {
			t.Fatalf("DefaultConfig().DomesticDomains missing %q", domain)
		}
	}
}

func TestDefaultDomesticDomainGroupsIncludeRequestedServices(t *testing.T) {
	groups := DefaultDomesticDomainGroups()
	got := map[string]bool{}
	for _, group := range groups {
		got[group.Key] = len(group.Domains) > 0
	}
	want := []string{
		"baidu",
		"sina_weibo",
		"wechat_tim",
		"bilibili",
		"alibaba_ecommerce",
		"jd",
		"xiaohongshu",
		"douyin",
		"pinduoduo",
		"banks",
		"domestic_games",
		"platform_11",
	}
	for _, key := range want {
		if !got[key] {
			t.Fatalf("DefaultDomesticDomainGroups missing non-empty group %q", key)
		}
	}
}

func TestNormalizeUpgradesLegacyDefaultDomesticDomains(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DomesticDomains = []string{
		"baidu.com",
		"bilibili.com",
		"qq.com",
		"taobao.com",
		"tmall.com",
		"jd.com",
		"alipay.com",
		"163.com",
		"douyin.com",
		"5211game.com",
		"www.5211game.com",
	}
	cfg.normalize()
	if len(cfg.DomesticDomains) <= 11 {
		t.Fatalf("normalize() DomesticDomains len = %d, want upgraded built-in whitelist", len(cfg.DomesticDomains))
	}
	if !containsString(cfg.DomesticDomains, "icbc.com.cn") {
		t.Fatal("normalize() did not upgrade legacy domestic domains to include bank whitelist")
	}
}

func TestNormalizeUpgradesLegacyDefaultDomesticDomainSubset(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DomesticDomains = []string{
		"baidu.com",
		"bilibili.com",
		"qq.com",
		"taobao.com",
		"tmall.com",
		"jd.com",
		"alipay.com",
		"163.com",
		"douyin.com",
	}
	cfg.normalize()
	if len(cfg.DomesticDomains) <= 9 {
		t.Fatalf("normalize() DomesticDomains len = %d, want upgraded built-in whitelist", len(cfg.DomesticDomains))
	}
	if !containsString(cfg.DomesticDomains, "douyin.com") {
		t.Fatal("normalize() did not upgrade legacy domestic domains to include requested whitelist")
	}
}

func TestNormalizeMergesCustomDomesticDomains(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DomesticDomains = []string{"example.internal", "baidu.com"}
	cfg.normalize()
	if !containsString(cfg.DomesticDomains, "example.internal") {
		t.Fatal("normalize() should keep custom domestic domain")
	}
	if !containsString(cfg.DomesticDomains, "weixin.qq.com") {
		t.Fatal("normalize() should merge built-in domestic domains")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestNormalizeTrafficBackend(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", TrafficBackendAuto},
		{"auto", "auto", TrafficBackendAuto},
		{"openconnect", "openconnect_tun", TrafficBackendOpenTun},
		{"cisco", "cisco_static", TrafficBackendCiscoStatic},
		{"invalid", "wfp", TrafficBackendAuto},
		{"spaced", " openconnect_tun ", TrafficBackendOpenTun},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.TrafficBackend = tt.in
			cfg.normalize()
			if cfg.TrafficBackend != tt.want {
				t.Fatalf("TrafficBackend = %q, want %q", cfg.TrafficBackend, tt.want)
			}
		})
	}
}

func TestNormalizeUsesGlobalCodexCandidatesWhenMissing(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CodexCandidateSites = nil
	cfg.normalize()
	if len(cfg.CodexCandidateSites) == 0 {
		t.Fatal("normalize() CodexCandidateSites is empty")
	}
	if strings.Contains(cfg.CodexCandidateSites[0], "国内") {
		t.Fatalf("normalize() CodexCandidateSites[0] = %q, want global site first", cfg.CodexCandidateSites[0])
	}
}

func TestDefaultConfigUsesDomesticDirectMode(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.SplitMode != SplitModeDomesticDirect {
		t.Fatalf("DefaultConfig().SplitMode = %q, want domestic_direct", cfg.SplitMode)
	}
	if !cfg.IsDomesticDirect() {
		t.Fatal("IsDomesticDirect() = false for default config")
	}
	if len(cfg.ForeignDomains) == 0 {
		t.Fatal("DefaultConfig().ForeignDomains is empty")
	}
	if cfg.RouteEntryLimit != DefaultRouteEntryLimit {
		t.Fatalf("DefaultConfig().RouteEntryLimit = %d, want %d", cfg.RouteEntryLimit, DefaultRouteEntryLimit)
	}
}

func TestNormalizeForcesSplitTunnelEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SplitTunnelEnabled = false

	cfg.normalize()

	if !cfg.SplitTunnelEnabled {
		t.Fatal("normalize() left split tunneling disabled; split tunneling must always be enabled")
	}
}

func TestNormalizeLegacyConfigDefaultsToForeignDirect(t *testing.T) {
	cfg := DefaultConfig()
	// Simulate an old config file that never had split_mode set.
	cfg.SplitMode = ""
	cfg.normalizeWith(false) // hasSplitMode=false => legacy
	if cfg.SplitMode != SplitModeForeignDirect {
		t.Fatalf("legacy normalize SplitMode = %q, want foreign_direct", cfg.SplitMode)
	}
	if cfg.IsDomesticDirect() {
		t.Fatal("legacy config should not be domestic direct")
	}
}

func TestAddRemoveForeignDomain(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ForeignDomains = nil
	if !cfg.AddForeignDomain("https://openai.com/path") {
		t.Fatal("AddForeignDomain should normalize and add")
	}
	if cfg.ForeignDomains[0] != "openai.com" {
		t.Fatalf("normalized domain = %q, want openai.com", cfg.ForeignDomains[0])
	}
	if cfg.AddForeignDomain("OPENAI.com") {
		t.Fatal("AddForeignDomain should dedupe case-insensitively")
	}
	if !cfg.RemoveForeignDomain("openai.com") {
		t.Fatal("RemoveForeignDomain should succeed")
	}
	if len(cfg.ForeignDomains) != 0 {
		t.Fatalf("ForeignDomains len = %d, want 0", len(cfg.ForeignDomains))
	}
	if cfg.AddForeignDomain("invalid") {
		t.Fatal("AddForeignDomain should reject bare word without a dot")
	}
}

func TestAddRemoveForeignCIDR(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ForeignCIDRs = nil
	if !cfg.AddForeignCIDR("1.2.3.4") {
		t.Fatal("AddForeignCIDR should accept a bare IP")
	}
	if cfg.ForeignCIDRs[0] != "1.2.3.4/32" {
		t.Fatalf("canonical CIDR = %q, want 1.2.3.4/32", cfg.ForeignCIDRs[0])
	}
	if cfg.AddForeignCIDR("1.2.3.4/32") {
		t.Fatal("AddForeignCIDR should dedupe canonical form")
	}
	if !cfg.AddForeignCIDR("5.6.0.0/16") {
		t.Fatal("AddForeignCIDR should accept a CIDR")
	}
	if !cfg.RemoveForeignCIDR("1.2.3.4") {
		t.Fatal("RemoveForeignCIDR should match by canonical form")
	}
	if cfg.RemoveForeignCIDR("9.9.9.9") {
		t.Fatal("RemoveForeignCIDR should report false for absent entry")
	}
}

func TestAddRemoveDomesticDomain(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DomesticDomains = nil
	if !cfg.AddDomesticDomain("https://bank.example.cn/app") {
		t.Fatal("AddDomesticDomain should normalize and add")
	}
	if cfg.DomesticDomains[0] != "bank.example.cn" {
		t.Fatalf("normalized domain = %q, want bank.example.cn", cfg.DomesticDomains[0])
	}
	if cfg.AddDomesticDomain("BANK.example.cn") {
		t.Fatal("AddDomesticDomain should dedupe case-insensitively")
	}
	if !cfg.RemoveDomesticDomain("bank.example.cn") {
		t.Fatal("RemoveDomesticDomain should succeed")
	}
	if len(cfg.DomesticDomains) != 0 {
		t.Fatalf("DomesticDomains len = %d, want 0", len(cfg.DomesticDomains))
	}
}

func TestAddRemoveDomesticCIDR(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DomesticCIDRs = nil
	if !cfg.AddDomesticCIDR("203.0.113.8") {
		t.Fatal("AddDomesticCIDR should accept a bare IP")
	}
	if cfg.DomesticCIDRs[0] != "203.0.113.8/32" {
		t.Fatalf("canonical CIDR = %q, want 203.0.113.8/32", cfg.DomesticCIDRs[0])
	}
	if cfg.AddDomesticCIDR("203.0.113.8/32") {
		t.Fatal("AddDomesticCIDR should dedupe canonical form")
	}
	if !cfg.RemoveDomesticCIDR("203.0.113.8") {
		t.Fatal("RemoveDomesticCIDR should match by canonical form")
	}
	if len(cfg.DomesticCIDRs) != 0 {
		t.Fatalf("DomesticCIDRs len = %d, want 0", len(cfg.DomesticCIDRs))
	}
}

func TestNormalizeSplitModeAcceptsSupportedValues(t *testing.T) {
	tests := map[string]string{
		" domestic_direct ": SplitModeDomesticDirect,
		"FOREIGN_DIRECT":    SplitModeForeignDirect,
	}
	for input, want := range tests {
		got, ok := NormalizeSplitMode(input)
		if !ok || got != want {
			t.Fatalf("NormalizeSplitMode(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
}

func TestNormalizeSplitModeRejectsUnknownValue(t *testing.T) {
	if got, ok := NormalizeSplitMode("unknown"); ok || got != "" {
		t.Fatalf("NormalizeSplitMode(unknown) = %q, %v; want empty, false", got, ok)
	}
}
