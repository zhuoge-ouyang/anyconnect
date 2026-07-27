package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDistConfigParsesWithNewFields(t *testing.T) {
	// Locate the dist template relative to the module root.
	candidates := []string{
		"../../configs/config.dist.yaml",
		"../../../configs/config.dist.yaml",
	}
	var path string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			path = filepath.Clean(c)
			break
		}
	}
	if path == "" {
		t.Skip("config.dist.yaml not found from test working dir")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dist config: %v", err)
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		t.Fatalf("dist config does not parse: %v", err)
	}
	if cfg.SplitMode != SplitModeDomesticDirect {
		t.Fatalf("dist SplitMode = %q, want domestic_direct", cfg.SplitMode)
	}
	if len(cfg.ForeignDomains) == 0 {
		t.Fatal("dist ForeignDomains is empty")
	}
	if len(cfg.DomesticDomains) == 0 {
		t.Fatal("dist DomesticDomains is empty")
	}
	if cfg.RouteEntryLimit != DefaultRouteEntryLimit {
		t.Fatalf("dist RouteEntryLimit = %d, want %d", cfg.RouteEntryLimit, DefaultRouteEntryLimit)
	}
	// Codex/GitHub/Claude must be present.
	want := []string{"openai.com", "chatgpt.com", "github.com", "gitlab.com", "anthropic.com", "claude.ai", "gemini.google.com"}
	set := map[string]struct{}{}
	for _, d := range cfg.ForeignDomains {
		set[d] = struct{}{}
	}
	for _, w := range want {
		if _, ok := set[w]; !ok {
			t.Fatalf("dist ForeignDomains missing %q", w)
		}
	}
	domesticWant := []string{"weixin.qq.com", "tim.qq.com", "bilibili.com", "taobao.com", "tmall.com", "jd.com", "goofish.com", "xiaohongshu.com", "douyin.com", "pinduoduo.com", "icbc.com.cn", "5211game.com"}
	domesticSet := map[string]struct{}{}
	for _, d := range cfg.DomesticDomains {
		domesticSet[d] = struct{}{}
	}
	for _, w := range domesticWant {
		if _, ok := domesticSet[w]; !ok {
			t.Fatalf("dist DomesticDomains missing %q", w)
		}
	}
}
