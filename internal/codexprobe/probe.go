package codexprobe

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const endpoint = "https://chatgpt.com/backend-api/codex/responses"
const apiEndpoint = "https://api.openai.com/v1/models"
const traceEndpoint = "https://chatgpt.com/cdn-cgi/trace"

type Result struct {
	Name        string
	Status      int
	Duration    time.Duration
	CFRay       string
	CFMitigated string
	Error       string
}

type ServiceRound struct {
	ChatGPT   Result
	OpenAIAPI Result
	ExitIP    string
	Region    string
	Duration  time.Duration
}

func (r ServiceRound) Healthy() bool { return r.ChatGPT.Healthy() && r.OpenAIAPI.Healthy() }
func (r ServiceRound) Blocked() bool { return r.ChatGPT.Blocked() || r.OpenAIAPI.Blocked() }

func (r Result) Healthy() bool {
	return r.Error == "" &&
		r.Status == http.StatusUnauthorized &&
		!strings.EqualFold(r.CFMitigated, "challenge")
}

func (r Result) Blocked() bool {
	return r.Status == http.StatusForbidden || strings.EqualFold(r.CFMitigated, "challenge")
}

func Probe(ctx context.Context) Result {
	return probeEndpoint(ctx, "ChatGPT", http.MethodPost, endpoint, []byte(`{}`))
}

func ProbeServices(ctx context.Context) ServiceRound {
	var chatgpt, api Result
	var wg sync.WaitGroup
	wg.Add(2)
	start := time.Now()
	go func() {
		defer wg.Done()
		chatgpt = probeEndpoint(ctx, "ChatGPT", http.MethodPost, endpoint, []byte(`{}`))
	}()
	go func() { defer wg.Done(); api = probeEndpoint(ctx, "OpenAI API", http.MethodGet, apiEndpoint, nil) }()
	wg.Wait()
	ip, region := probeTrace(ctx)
	return ServiceRound{ChatGPT: chatgpt, OpenAIAPI: api, ExitIP: ip, Region: region, Duration: time.Since(start)}
}

func probeEndpoint(ctx context.Context, name, method, url string, body []byte) Result {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return Result{Name: name, Error: err.Error()}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "anyconnect-split-codex-probe/1.0")

	start := time.Now()
	resp, err := client().Do(req)
	duration := time.Since(start)
	if err != nil {
		return Result{Name: name, Duration: duration, Error: err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2048))

	return Result{
		Name:        name,
		Status:      resp.StatusCode,
		Duration:    duration,
		CFRay:       resp.Header.Get("CF-RAY"),
		CFMitigated: resp.Header.Get("Cf-Mitigated"),
	}
}

func probeTrace(ctx context.Context) (string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, traceEndpoint, nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("User-Agent", "anyconnect-split-codex-probe/1.0")
	resp, err := client().Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", ""
	}
	return parseTrace(string(data))
}

func parseTrace(data string) (string, string) {
	values := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = strings.TrimSpace(parts[1])
		}
	}
	return values["ip"], values["colo"]
}

func Score(results []Result) int {
	if len(results) == 0 {
		return -100000
	}
	score := 0
	for _, result := range results {
		switch {
		case result.Blocked():
			score -= 100000
		case result.Error != "":
			score -= 30000
		case result.Healthy():
			score += 10000
		default:
			score -= 5000
		}
	}
	median := MedianDuration(results)
	if median <= 0 {
		return score
	}
	return score - int(median/time.Millisecond)
}

func MedianDuration(results []Result) time.Duration {
	durations := make([]time.Duration, 0, len(results))
	for _, result := range results {
		if result.Error == "" && result.Duration > 0 {
			durations = append(durations, result.Duration)
		}
	}
	if len(durations) == 0 {
		return 0
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	return durations[len(durations)/2]
}

func AllHealthy(results []Result) bool {
	if len(results) == 0 {
		return false
	}
	for _, result := range results {
		if !result.Healthy() {
			return false
		}
	}
	return true
}

func client() *http.Client {
	dialer := &net.Dialer{
		Timeout:   8 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy:       http.ProxyFromEnvironment,
			DialContext: dialer.DialContext,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          4,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   8 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}
