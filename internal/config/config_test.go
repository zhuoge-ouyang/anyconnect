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

func TestDefaultDomesticDomainsEmpty(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.DomesticDomains) != 0 {
		t.Fatalf("DefaultConfig().DomesticDomains len = %d, want 0", len(cfg.DomesticDomains))
	}
}

func TestNormalizeClearsLegacyDefaultDomesticDomains(t *testing.T) {
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
	if len(cfg.DomesticDomains) != 0 {
		t.Fatalf("normalize() DomesticDomains len = %d, want 0", len(cfg.DomesticDomains))
	}
}

func TestNormalizeClearsLegacyDefaultDomesticDomainSubset(t *testing.T) {
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
	if len(cfg.DomesticDomains) != 0 {
		t.Fatalf("normalize() DomesticDomains len = %d, want 0", len(cfg.DomesticDomains))
	}
}

func TestNormalizeKeepsCustomDomesticDomains(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DomesticDomains = []string{"example.internal", "baidu.com"}
	cfg.normalize()
	if len(cfg.DomesticDomains) != 2 {
		t.Fatalf("normalize() DomesticDomains len = %d, want 2", len(cfg.DomesticDomains))
	}
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
