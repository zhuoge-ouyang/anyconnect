package config

import (
	"net"
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
	DefaultRouteEntryLimit     = 2000

	// SplitModeDomesticDirect: 默认全部直连，仅国外白名单走 VPN。
	SplitModeDomesticDirect = "domestic_direct"
	// SplitModeForeignDirect: 默认全部走 VPN，仅国内白名单直连（兼容旧行为）。
	SplitModeForeignDirect = "foreign_direct"
)

type VPNSite struct {
	Name   string `yaml:"name"`   // 站点名称，如 "深圳"、"香港"、"美国"、"日本"
	Server string `yaml:"server"` // 服务器地址
}

type DomainGroup struct {
	Key     string
	Name    string
	Domains []string
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
	CodexModeActive        bool      `yaml:"codex_mode_active"`
	CodexPreferredSite     string    `yaml:"codex_preferred_site"`
	CodexProbeAttempts     int       `yaml:"codex_probe_attempts"`
	CodexCandidateSites    []string  `yaml:"codex_candidate_sites"`
	DomesticDomains        []string  `yaml:"domestic_domain_exceptions"`
	DomesticCIDRs          []string  `yaml:"domestic_ip_exceptions"`
	ForeignDomains         []string  `yaml:"foreign_domain_exceptions"`
	ForeignCIDRs           []string  `yaml:"foreign_ip_exceptions"`
	SplitMode              string    `yaml:"split_mode"`
	RouteEntryLimit        int       `yaml:"route_entry_limit"`
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
		SplitTunnelEnabled:  true,
		IPv6SplitEnabled:    false,
		AutoStart:           false,
		AutoConnect:         false,
		PreferredSite:       DefaultGlobalPreferredSite,
		CodexAutoSelect:     true,
		CodexPreferredSite:  DefaultGlobalPreferredSite,
		CodexProbeAttempts:  2,
		CodexCandidateSites: defaultCodexCandidateSites(),
		DomesticDomains:     defaultDomesticDomains(),
		UpdateIntervalDays:  7,
		LogLevel:            "warn",
		VPNSites:            defaultVPNSites(),
		TrafficBackend:      TrafficBackendAuto,
		SplitMode:           SplitModeDomesticDirect,
		ForeignDomains:      defaultForeignDomains(),
		RouteEntryLimit:     DefaultRouteEntryLimit,
	}
}

func DefaultDomesticDomainGroups() []DomainGroup {
	groups := make([]DomainGroup, 0, len(builtInDomesticDomainGroups))
	for _, group := range builtInDomesticDomainGroups {
		groups = append(groups, DomainGroup{
			Key:     group.Key,
			Name:    group.Name,
			Domains: append([]string(nil), group.Domains...),
		})
	}
	return groups
}

func defaultDomesticDomains() []string {
	seen := map[string]struct{}{}
	domains := []string{}
	for _, group := range builtInDomesticDomainGroups {
		for _, raw := range group.Domains {
			domain := NormalizeDomain(raw)
			if domain == "" {
				continue
			}
			if _, ok := seen[domain]; ok {
				continue
			}
			seen[domain] = struct{}{}
			domains = append(domains, domain)
		}
	}
	return domains
}

var builtInDomesticDomainGroups = []DomainGroup{
	{
		Key:  "baidu",
		Name: "百度",
		Domains: []string{
			"baidu.com",
			"bdstatic.com",
			"bdimg.com",
			"baidubce.com",
			"hao123.com",
		},
	},
	{
		Key:  "sina_weibo",
		Name: "新浪/微博",
		Domains: []string{
			"sina.com",
			"sina.com.cn",
			"sinaimg.cn",
			"weibo.com",
			"weibo.cn",
			"t.cn",
		},
	},
	{
		Key:  "wechat_tim",
		Name: "微信/TIM/腾讯基础服务",
		Domains: []string{
			"qq.com",
			"wechat.com",
			"weixin.qq.com",
			"wx.qq.com",
			"mp.weixin.qq.com",
			"res.wx.qq.com",
			"weixin110.qq.com",
			"tim.qq.com",
			"gtimg.cn",
			"gtimg.com",
			"qpic.cn",
			"idqqimg.com",
			"qlogo.cn",
			"myqcloud.com",
		},
	},
	{
		Key:  "bilibili",
		Name: "哔哩哔哩",
		Domains: []string{
			"bilibili.com",
			"bilibili.tv",
			"biligame.com",
			"biliapi.net",
			"bilivideo.com",
			"hdslb.com",
			"b23.tv",
		},
	},
	{
		Key:  "alibaba_ecommerce",
		Name: "淘宝/天猫/闲鱼/阿里电商",
		Domains: []string{
			"taobao.com",
			"tmall.com",
			"tmall.hk",
			"goofish.com",
			"2.taobao.com",
			"alipay.com",
			"alicdn.com",
			"tbcdn.cn",
			"mmstat.com",
			"alibaba.com",
			"alibaba-inc.com",
			"alibabausercontent.com",
			"aliyuncs.com",
		},
	},
	{
		Key:  "jd",
		Name: "京东",
		Domains: []string{
			"jd.com",
			"360buy.com",
			"360buyimg.com",
			"jdcdn.com",
			"jcloud.com",
			"jdcloud.com",
		},
	},
	{
		Key:  "xiaohongshu",
		Name: "小红书",
		Domains: []string{
			"xiaohongshu.com",
			"xhscdn.com",
			"xhslink.com",
		},
	},
	{
		Key:  "douyin",
		Name: "抖音/字节",
		Domains: []string{
			"douyin.com",
			"iesdouyin.com",
			"douyinpic.com",
			"douyinvod.com",
			"douyinstatic.com",
			"amemv.com",
			"snssdk.com",
			"bytedance.com",
			"byteimg.com",
			"bytednsdoc.com",
			"toutiao.com",
		},
	},
	{
		Key:  "pinduoduo",
		Name: "拼多多",
		Domains: []string{
			"pinduoduo.com",
			"yangkeduo.com",
			"pddpic.com",
			"pddcdn.com",
		},
	},
	{
		Key:  "banks",
		Name: "常用银行",
		Domains: []string{
			"icbc.com.cn",
			"abchina.com",
			"boc.cn",
			"bankofchina.com",
			"ccb.com",
			"bankcomm.com",
			"psbc.com",
			"cmbchina.com",
			"cmbc.com.cn",
			"cebbank.com",
			"citicbank.com",
			"spdb.com.cn",
			"cib.com.cn",
			"hxb.com.cn",
			"bank.pingan.com",
			"pingan.com",
			"cgbchina.com.cn",
			"bankofbeijing.com.cn",
			"bankofshanghai.com",
			"nbcb.com.cn",
			"srcb.com",
			"hsbank.com.cn",
			"czbank.com",
			"hrbb.com.cn",
		},
	},
	{
		Key:  "domestic_games",
		Name: "国内游戏公司",
		Domains: []string{
			"game.qq.com",
			"wegame.com.cn",
			"start.qq.com",
			"tencent.com",
			"163.com",
			"netease.com",
			"neteasegames.com",
			"game.163.com",
			"mihoyo.com",
			"mhyurl.cn",
			"yuanshen.com",
			"hoyoverse.com",
			"hoyolab.com",
			"bhsr.com",
			"wanmei.com",
			"perfectworld.com",
			"37.com",
			"4399.com",
			"xoyo.com",
			"seasunwbl.com",
			"ztgame.com",
			"youzu.com",
			"lilith.com",
		},
	},
	{
		Key:  "platform_11",
		Name: "11 对战平台",
		Domains: []string{
			"5211game.com",
			"www.5211game.com",
		},
	},
}

func defaultForeignDomains() []string {
	return []string{
		// 视频
		"pornhub.com",
		"youtube.com",
		"ytimg.com",
		// Google
		"google.com",
		"googleapis.com",
		"googlevideo.com",
		"gstatic.com",
		"ggpht.com",
		// Gemini
		"gemini.google.com",
		"aistudio.google.com",
		"generativelanguage.googleapis.com",
		// OpenAI / Codex / ChatGPT
		"openai.com",
		"chatgpt.com",
		"api.openai.com",
		"auth.openai.com",
		"platform.openai.com",
		"cdn.openai.com",
		"files.oaiusercontent.com",
		"chat.com",
		"oaistatic.com",
		"oaiusercontent.com",
		// Claude / Anthropic
		"anthropic.com",
		"claude.ai",
		"api.anthropic.com",
		// GitHub / Copilot
		"github.com",
		"githubusercontent.com",
		"githubassets.com",
		"githubcopilot.com",
		"copilot.microsoft.com",
		// GitLab
		"gitlab.com",
		"gitlab-static.net",
	}
}

func defaultCodexCandidateSites() []string {
	return []string{
		"23.澳大利亚",
		"22.日本",
		"21.韩国",
		"20.泰国",
		"25.英国",
		"24.美国",
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
	splitWasDisabled := !cfg.SplitTunnelEnabled
	// Detect whether split_mode was explicitly set in the config file.
	// Old configs without the key preserve the legacy foreign_direct behavior.
	var raw map[string]any
	_ = yaml.Unmarshal(data, &raw)
	_, hasSplitMode := raw["split_mode"]
	cfg.normalizeWith(hasSplitMode)
	if cfg.VPNCLIPath == "" {
		cfg.VPNCLIPath = DetectVPNCLIPath()
	}
	if splitWasDisabled {
		_ = cfg.Save()
	}
	return cfg, nil
}

func (c *Config) normalize() {
	c.normalizeWith(true)
}

func (c *Config) normalizeWith(hasSplitMode bool) {
	// Split tunneling is a product invariant. Users choose the default route
	// mode, but cannot turn split routing into a system-wide VPN tunnel.
	c.SplitTunnelEnabled = true
	if len(c.VPNSites) == 0 {
		c.VPNSites = defaultVPNSites()
	}
	if c.UpdateIntervalDays <= 0 {
		c.UpdateIntervalDays = 7
	}
	if c.RouteEntryLimit <= 0 {
		c.RouteEntryLimit = DefaultRouteEntryLimit
	}
	if c.LogLevel == "" {
		c.LogLevel = "warn"
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
	rawSplitMode := strings.TrimSpace(c.SplitMode)
	if rawSplitMode == "" {
		// Key missing on an existing config file => legacy install, keep
		// foreign_direct behavior. New installs (DefaultConfig) already set
		// domestic_direct and will persist it, so the key will be present on
		// subsequent loads.
		if !hasSplitMode {
			c.SplitMode = SplitModeForeignDirect
		} else {
			c.SplitMode = SplitModeDomesticDirect
		}
	} else if normalized, ok := NormalizeSplitMode(rawSplitMode); ok {
		c.SplitMode = normalized
	} else {
		c.SplitMode = SplitModeForeignDirect
	}
	if c.SplitMode == SplitModeDomesticDirect && len(c.ForeignDomains) == 0 {
		c.ForeignDomains = defaultForeignDomains()
	}
	if c.CodexPreferredSite == "" {
		c.CodexPreferredSite = DefaultGlobalPreferredSite
	}
	if c.CodexProbeAttempts <= 0 {
		c.CodexProbeAttempts = 2
	}
	if len(c.CodexCandidateSites) == 0 {
		c.CodexCandidateSites = defaultCodexCandidateSites()
	}
	if isLegacyDefaultDomesticDomains(c.DomesticDomains) {
		c.DomesticDomains = defaultDomesticDomains()
	}
	c.DomesticDomains = mergeDefaultDomesticDomains(c.DomesticDomains)
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

func mergeDefaultDomesticDomains(domains []string) []string {
	merged := defaultDomesticDomains()
	seen := make(map[string]struct{}, len(merged)+len(domains))
	for _, domain := range merged {
		seen[domain] = struct{}{}
	}
	for _, domain := range domains {
		domain = NormalizeDomain(domain)
		if domain == "" {
			continue
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		merged = append(merged, domain)
	}
	return merged
}

func (c *Config) Save() error {
	c.SplitTunnelEnabled = true
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

// IsDomesticDirect reports whether the split mode defaults traffic to direct
// (only foreign whitelist goes through VPN).
func (c *Config) IsDomesticDirect() bool {
	return strings.EqualFold(strings.TrimSpace(c.SplitMode), SplitModeDomesticDirect)
}

// NormalizeSplitMode canonicalizes a supported split mode. Unknown values are
// rejected so interactive controls cannot silently select a different mode.
func NormalizeSplitMode(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case SplitModeDomesticDirect:
		return SplitModeDomesticDirect, true
	case SplitModeForeignDirect:
		return SplitModeForeignDirect, true
	default:
		return "", false
	}
}

// NormalizeDomain strips protocol/path/port and returns a bare domain suffix,
// or empty if the input is not a plausible domain.
func NormalizeDomain(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "https://")
	if idx := strings.IndexAny(raw, "/?#"); idx >= 0 {
		raw = raw[:idx]
	}
	if idx := strings.LastIndex(raw, ":"); idx >= 0 {
		// strip port if present (but keep for IPv6 literals handled elsewhere)
		if !strings.Contains(raw[idx+1:], ":") {
			raw = raw[:idx]
		}
	}
	raw = strings.ToLower(strings.Trim(raw, "."))
	if raw == "" || !strings.Contains(raw, ".") {
		return ""
	}
	return raw
}

// AddForeignDomain appends a foreign domain to the whitelist (deduplicated).
// Returns true if the list changed.
func (c *Config) AddForeignDomain(domain string) bool {
	domain = NormalizeDomain(domain)
	if domain == "" {
		return false
	}
	for _, existing := range c.ForeignDomains {
		if existing == domain {
			return false
		}
	}
	c.ForeignDomains = append(c.ForeignDomains, domain)
	return true
}

// AddDomesticDomain appends a domestic domain to the whitelist (deduplicated).
// Returns true if the list changed.
func (c *Config) AddDomesticDomain(domain string) bool {
	domain = NormalizeDomain(domain)
	if domain == "" {
		return false
	}
	for _, existing := range c.DomesticDomains {
		if existing == domain {
			return false
		}
	}
	c.DomesticDomains = append(c.DomesticDomains, domain)
	return true
}

// RemoveDomesticDomain removes a domestic domain from the whitelist.
func (c *Config) RemoveDomesticDomain(domain string) bool {
	domain = NormalizeDomain(domain)
	if domain == "" {
		return false
	}
	for i, existing := range c.DomesticDomains {
		if existing == domain {
			c.DomesticDomains = append(c.DomesticDomains[:i], c.DomesticDomains[i+1:]...)
			return true
		}
	}
	return false
}

// RemoveForeignDomain removes a foreign domain from the whitelist.
func (c *Config) RemoveForeignDomain(domain string) bool {
	domain = NormalizeDomain(domain)
	if domain == "" {
		return false
	}
	for i, existing := range c.ForeignDomains {
		if existing == domain {
			c.ForeignDomains = append(c.ForeignDomains[:i], c.ForeignDomains[i+1:]...)
			return true
		}
	}
	return false
}

// AddForeignCIDR appends a foreign IP/CIDR to the whitelist (deduplicated,
// canonicalized). Returns true if the list changed.
func (c *Config) AddForeignCIDR(cidr string) bool {
	canon, ok := canonicalCIDR(cidr)
	if !ok {
		return false
	}
	for _, existing := range c.ForeignCIDRs {
		if existing == canon {
			return false
		}
	}
	c.ForeignCIDRs = append(c.ForeignCIDRs, canon)
	return true
}

// AddDomesticCIDR appends a domestic IP/CIDR to the whitelist (deduplicated,
// canonicalized). Returns true if the list changed.
func (c *Config) AddDomesticCIDR(cidr string) bool {
	canon, ok := canonicalCIDR(cidr)
	if !ok {
		return false
	}
	for _, existing := range c.DomesticCIDRs {
		if existing == canon {
			return false
		}
	}
	c.DomesticCIDRs = append(c.DomesticCIDRs, canon)
	return true
}

// RemoveDomesticCIDR removes a domestic IP/CIDR from the whitelist.
func (c *Config) RemoveDomesticCIDR(cidr string) bool {
	canon, ok := canonicalCIDR(cidr)
	if !ok {
		return false
	}
	for i, existing := range c.DomesticCIDRs {
		if existing == canon {
			c.DomesticCIDRs = append(c.DomesticCIDRs[:i], c.DomesticCIDRs[i+1:]...)
			return true
		}
	}
	return false
}

// RemoveForeignCIDR removes a foreign IP/CIDR from the whitelist.
func (c *Config) RemoveForeignCIDR(cidr string) bool {
	canon, ok := canonicalCIDR(cidr)
	if !ok {
		return false
	}
	for i, existing := range c.ForeignCIDRs {
		if existing == canon {
			c.ForeignCIDRs = append(c.ForeignCIDRs[:i], c.ForeignCIDRs[i+1:]...)
			return true
		}
	}
	return false
}

func canonicalCIDR(cidr string) (string, bool) {
	cidr = strings.TrimSpace(cidr)
	if !strings.Contains(cidr, "/") {
		ip := net.ParseIP(cidr)
		if ip == nil {
			return "", false
		}
		if v4 := ip.To4(); v4 != nil {
			return v4.String() + "/32", true
		}
		return ip.To16().String() + "/128", true
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", false
	}
	return ipNet.String(), true
}
