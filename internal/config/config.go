package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	DefaultGlobalPreferredSite = "23.澳大利亚"
	TrafficBackendAuto         = "auto"
	TrafficBackendOpenTun      = "openconnect_tun"
	TrafficBackendCiscoStatic  = "cisco_static"
)

type VPNSite struct {
	Name   string `yaml:"name"`   // 站点名称，如 "深圳"、"香港"、"美国"、"日本"
	Server string `yaml:"server"` // 服务器地址
}

type Config struct {
	OriginalGateway        string    `yaml:"original_gateway"`
	OriginalInterfaceIndex int       `yaml:"original_interface_index"`
	OriginalIPv6Gateway    string    `yaml:"original_ipv6_gateway"`
	OriginalIPv6Interface  int       `yaml:"original_ipv6_interface_index"`
	SplitTunnelEnabled     bool      `yaml:"split_tunnel_enabled"`
	IPv6SplitEnabled       bool      `yaml:"ipv6_split_enabled"`
	AutoStart              bool      `yaml:"auto_start"`
	AutoConnect            bool      `yaml:"auto_connect"`
	PreferredSite          string    `yaml:"preferred_site"`
	SavedUsername          string    `yaml:"saved_username"`
	CodexAutoSelect        bool      `yaml:"codex_auto_select"`
	CodexPreferredSite     string    `yaml:"codex_preferred_site"`
	CodexProbeAttempts     int       `yaml:"codex_probe_attempts"`
	CodexCandidateSites    []string  `yaml:"codex_candidate_sites"`
	DomesticDomains        []string  `yaml:"domestic_domain_exceptions"`
	UpdateIntervalDays     int       `yaml:"update_interval_days"`
	LastUpdate             time.Time `yaml:"last_update"`
	LogLevel               string    `yaml:"log_level"`
	VPNSites               []VPNSite `yaml:"vpn_sites"` // 多个 VPN 站点配置
	VPNCLIPath             string    `yaml:"vpncli_path"`
	TrafficBackend         string    `yaml:"traffic_backend"`
	OpenConnectPath        string    `yaml:"openconnect_path"`
	SingBoxPath            string    `yaml:"sing_box_path"`
}

func DefaultConfig() *Config {
	return &Config{
		SplitTunnelEnabled: true,
		IPv6SplitEnabled:   false,
		AutoStart:          false,
		AutoConnect:        false,
		PreferredSite:      DefaultGlobalPreferredSite,
		CodexAutoSelect:    true,
		CodexPreferredSite: DefaultGlobalPreferredSite,
		CodexProbeAttempts: 2,
		CodexCandidateSites: []string{
			"03.国内专线-深圳节点",
			"05.国内专线-贵州节点",
			"20.泰国",
			"21.韩国",
			"22.日本",
			"23.澳大利亚",
			"25.英国",
			"24.美国",
		},
		UpdateIntervalDays: 7,
		LogLevel:           "info",
		VPNSites:           defaultVPNSites(),
		TrafficBackend:     TrafficBackendAuto,
	}
}

func defaultVPNSites() []VPNSite {
	return []VPNSite{
		{Name: "01.国内专线-上海节点", Server: "https://api008621.ciscovnp.com:10000"},
		{Name: "02.国内专线-杭州节点", Server: "https://api008621.ciscovnp.com:10000"},
		{Name: "03.国内专线-深圳节点", Server: "https://api008620.ciscovnp.com:10000"},
		{Name: "04.国内专线-北京节点", Server: "https://api008610.ciscovnp.com:10000"},
		{Name: "05.国内专线-贵州节点", Server: "https://api008620.ciscovnp.com:10000"},
		{Name: "06.国内专线-徐州节点", Server: "https://api008621.ciscovnp.com:10000"},
		{Name: "10.香港地区", Server: "https://api0852.ciscovnp.com:10000"},
		{Name: "11.台湾地区", Server: "https://api0886.ciscovnp.com:10000"},
		{Name: "19.印度尼西亚", Server: "https://api0062.ciscovnp.com:10000"},
		{Name: "20.泰国", Server: "https://api0066.ciscovnp.com:10000"},
		{Name: "21.韩国", Server: "https://api0082.ciscovnp.com:10000"},
		{Name: "22.日本", Server: "https://api0081.ciscovnp.com:10000"},
		{Name: "23.澳大利亚", Server: "https://api0061.ciscovnp.com:10000"},
		{Name: "24.美国", Server: "https://api0001.ciscovnp.com:10000"},
		{Name: "25.英国", Server: "https://api0044.ciscovnp.com:10000"},
		{Name: "26.加拿大", Server: "https://api0001.ciscovnp.com:10000"},
		{Name: "27.土耳其", Server: "https://api0090.ciscovnp.com:10000"},
		{Name: "28.南非", Server: "https://api0027.ciscovnp.com:10000"},
	}
}

func configPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "configs", "config.yaml")
}

// DetectVPNCLIPath 自动检测 vpncli.exe 路径
func DetectVPNCLIPath() string {
	paths := []string{
		`C:\Program Files (x86)\Cisco\Cisco AnyConnect Secure Mobility Client\vpncli.exe`,
		`C:\Program Files\Cisco\Cisco AnyConnect Secure Mobility Client\vpncli.exe`,
		`C:\Program Files (x86)\Cisco\Cisco Secure Client\vpncli.exe`,
		`C:\Program Files\Cisco\Cisco Secure Client\vpncli.exe`,
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func Load() (*Config, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			_ = cfg.Save()
			return cfg, nil
		}
		return nil, err
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.normalize()
	if cfg.VPNCLIPath == "" {
		cfg.VPNCLIPath = DetectVPNCLIPath()
	}
	return cfg, nil
}

func (c *Config) normalize() {
	if len(c.VPNSites) == 0 {
		c.VPNSites = defaultVPNSites()
	}
	if c.UpdateIntervalDays <= 0 {
		c.UpdateIntervalDays = 7
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
	switch strings.ToLower(strings.TrimSpace(c.TrafficBackend)) {
	case "", TrafficBackendAuto:
		c.TrafficBackend = TrafficBackendAuto
	case TrafficBackendOpenTun:
		c.TrafficBackend = TrafficBackendOpenTun
	case TrafficBackendCiscoStatic:
		c.TrafficBackend = TrafficBackendCiscoStatic
	default:
		c.TrafficBackend = TrafficBackendAuto
	}
	if c.CodexPreferredSite == "" {
		c.CodexPreferredSite = DefaultGlobalPreferredSite
	}
	if c.CodexProbeAttempts <= 0 {
		c.CodexProbeAttempts = 2
	}
	if len(c.CodexCandidateSites) == 0 {
		c.CodexCandidateSites = []string{
			"03.国内专线-深圳节点",
			"05.国内专线-贵州节点",
			"20.泰国",
			"21.韩国",
			"22.日本",
			"23.澳大利亚",
			"25.英国",
			"24.美国",
		}
	}
	if isLegacyDefaultDomesticDomains(c.DomesticDomains) {
		c.DomesticDomains = nil
	}
	if c.PreferredSite == "" || !utf8.ValidString(c.PreferredSite) {
		c.PreferredSite = defaultPreferredSite(c.VPNSites)
		return
	}
	for _, site := range c.VPNSites {
		if site.Name == c.PreferredSite || strings.Contains(site.Name, c.PreferredSite) {
			return
		}
	}
}

func defaultPreferredSite(sites []VPNSite) string {
	for _, site := range sites {
		if site.Name == DefaultGlobalPreferredSite {
			return site.Name
		}
	}
	for _, hint := range []string{"香港", "台湾", "日本", "韩国", "泰国", "澳大利亚", "英国", "美国", "加拿大"} {
		for _, site := range sites {
			if strings.Contains(site.Name, hint) {
				return site.Name
			}
		}
	}
	if len(sites) > 0 {
		return sites[0].Name
	}
	return DefaultGlobalPreferredSite
}

func isLegacyDefaultDomesticDomains(domains []string) bool {
	legacy := []string{
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
	if len(domains) == 0 || len(domains) > len(legacy) {
		return false
	}
	legacySet := make(map[string]struct{}, len(legacy))
	for _, domain := range legacy {
		legacySet[domain] = struct{}{}
	}
	seen := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			return false
		}
		if _, ok := legacySet[domain]; !ok {
			return false
		}
		if _, duplicate := seen[domain]; duplicate {
			return false
		}
		seen[domain] = struct{}{}
	}
	return true
}

func (c *Config) Save() error {
	path := configPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
