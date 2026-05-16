package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/user/anyconnect-split/internal/config"
	"github.com/user/anyconnect-split/internal/credential"
	"github.com/user/anyconnect-split/internal/vpn"
)

const credentialTarget = "AnyConnectSplitTunnel"

type fileConfig struct {
	Sites      []config.VPNSite `yaml:"vpn_sites"`
	VPNCLIPath string           `yaml:"vpncli_path"`
}

type endpointProbe struct {
	Status      int
	Duration    time.Duration
	CFRay       string
	CFMitigated string
	Error       string
}

type traceProbe struct {
	IP      string `json:"ip"`
	Colo    string `json:"colo"`
	Loc     string `json:"loc"`
	HTTP    string `json:"http"`
	TLS     string `json:"tls"`
	Warp    string `json:"warp"`
	Gateway string `json:"gateway"`
	Error   string `json:"error"`
}

type siteResult struct {
	Names       []string
	Server      string
	Host        string
	ConnectOK   bool
	ConnectTime time.Duration
	Trace       traceProbe
	Probes      []endpointProbe
	Score       int
}

func main() {
	log.SetFlags(log.Ltime)

	configPath := flag.String("config", `D:\project\anyconnect\bin\configs\config.yaml`, "path to config.yaml")
	attempts := flag.Int("attempts", 3, "Codex endpoint probe attempts per site")
	keepBest := flag.Bool("keep-best", true, "reconnect to the best site after testing")
	filter := flag.String("filter", "", "site name substring filter")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fatalJSON(err)
	}
	if cfg.VPNCLIPath == "" {
		cfg.VPNCLIPath = config.DetectVPNCLIPath()
	}
	if cfg.VPNCLIPath == "" {
		fatalJSON(fmt.Errorf("vpncli.exe not found"))
	}

	username, password, err := credential.Read(credentialTarget)
	if err != nil || username == "" || password == "" {
		fatalJSON(fmt.Errorf("saved credential %q unavailable: %w", credentialTarget, err))
	}

	sites := dedupeSites(cfg.Sites, *filter)
	if len(sites) == 0 {
		fatalJSON(fmt.Errorf("no sites matched"))
	}

	results := make([]siteResult, 0, len(sites))
	for i, site := range sites {
		fmt.Fprintf(os.Stderr, "[%d/%d] testing %s (%s)\n", i+1, len(sites), strings.Join(site.Names, " / "), site.Host)
		result := testSite(cfg.VPNCLIPath, site, username, password, *attempts)
		results = append(results, result)
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return medianDuration(results[i].Probes) < medianDuration(results[j].Probes)
	})

	if *keepBest && len(results) > 0 && results[0].ConnectOK {
		best := results[0]
		fmt.Fprintf(os.Stderr, "reconnecting best site: %s\n", strings.Join(best.Names, " / "))
		_ = vpn.Disconnect(cfg.VPNCLIPath)
		if err := vpn.Connect(cfg.VPNCLIPath, best.Server, username, password); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to reconnect best site: %v\n", err)
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		fatalJSON(err)
	}
}

func fatalJSON(err error) {
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"error": err.Error()})
	os.Exit(1)
}

func loadConfig(path string) (fileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileConfig{}, err
	}
	var cfg fileConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fileConfig{}, err
	}
	return cfg, nil
}

type groupedSite struct {
	Names  []string
	Server string
	Host   string
}

func dedupeSites(sites []config.VPNSite, filter string) []groupedSite {
	byServer := map[string]*groupedSite{}
	order := []string{}
	for _, site := range sites {
		if filter != "" && !strings.Contains(site.Name, filter) {
			continue
		}
		host := serverHost(site.Server)
		key := site.Server
		if key == "" {
			continue
		}
		group, ok := byServer[key]
		if !ok {
			group = &groupedSite{Server: site.Server, Host: host}
			byServer[key] = group
			order = append(order, key)
		}
		group.Names = append(group.Names, site.Name)
	}

	out := make([]groupedSite, 0, len(order))
	for _, key := range order {
		out = append(out, *byServer[key])
	}
	return out
}

func serverHost(server string) string {
	parsed, err := url.Parse(server)
	if err != nil {
		return server
	}
	return parsed.Hostname()
}

func testSite(cliPath string, site groupedSite, username, password string, attempts int) siteResult {
	result := siteResult{
		Names:  site.Names,
		Server: site.Server,
		Host:   site.Host,
	}

	_ = vpn.Disconnect(cliPath)
	start := time.Now()
	if err := vpn.Connect(cliPath, site.Server, username, password); err != nil {
		result.ConnectOK = false
		result.Score = -1000
		result.Trace.Error = err.Error()
		return result
	}
	result.ConnectOK = true
	result.ConnectTime = time.Since(start)

	time.Sleep(2 * time.Second)
	result.Trace = cloudflareTrace()
	for i := 0; i < attempts; i++ {
		result.Probes = append(result.Probes, probeCodexEndpoint())
		time.Sleep(700 * time.Millisecond)
	}
	result.Score = scoreResult(result)
	return result
}

func httpClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   8 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:       http.ProxyFromEnvironment,
			DialContext: dialer.DialContext,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          10,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   8 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

func cloudflareTrace() traceProbe {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.cloudflare.com/cdn-cgi/trace", nil)
	if err != nil {
		return traceProbe{Error: err.Error()}
	}
	resp, err := httpClient(15 * time.Second).Do(req)
	if err != nil {
		return traceProbe{Error: err.Error()}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	trace := traceProbe{}
	for _, line := range strings.Split(string(body), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "ip":
			trace.IP = maskIP(value)
		case "colo":
			trace.Colo = value
		case "loc":
			trace.Loc = value
		case "http":
			trace.HTTP = value
		case "tls":
			trace.TLS = value
		case "warp":
			trace.Warp = value
		case "gateway":
			trace.Gateway = value
		}
	}
	return trace
}

func probeCodexEndpoint() endpointProbe {
	payload := []byte(`{}`)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", bytes.NewReader(payload))
	if err != nil {
		return endpointProbe{Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codex-sitecheck/1.0")

	start := time.Now()
	resp, err := httpClient(20 * time.Second).Do(req)
	duration := time.Since(start)
	if err != nil {
		return endpointProbe{Duration: duration, Error: err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2048))

	return endpointProbe{
		Status:      resp.StatusCode,
		Duration:    duration,
		CFRay:       resp.Header.Get("CF-RAY"),
		CFMitigated: resp.Header.Get("Cf-Mitigated"),
	}
}

func scoreResult(result siteResult) int {
	if !result.ConnectOK {
		return -1000
	}
	score := 0
	if result.Trace.Error != "" {
		score -= 5000
	}
	for _, probe := range result.Probes {
		switch {
		case probe.Status == http.StatusForbidden:
			score -= 100000
		case strings.EqualFold(probe.CFMitigated, "challenge"):
			score -= 100000
		case probe.Error != "":
			score -= 30000
		case probe.Status == http.StatusUnauthorized:
			score += 10000
		default:
			score -= 5000
		}
	}
	return score - int(medianDuration(result.Probes)/time.Millisecond)
}

func medianDuration(probes []endpointProbe) time.Duration {
	var durations []time.Duration
	for _, probe := range probes {
		if probe.Error == "" && probe.Duration > 0 {
			durations = append(durations, probe.Duration)
		}
	}
	if len(durations) == 0 {
		return time.Hour
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	return durations[len(durations)/2]
}

func maskIP(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip
	}
	if v4 := parsed.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.x.x", v4[0], v4[1])
	}
	parts := strings.Split(parsed.String(), ":")
	if len(parts) <= 2 {
		return parsed.String()
	}
	return strings.Join(parts[:2], ":") + "::"
}
