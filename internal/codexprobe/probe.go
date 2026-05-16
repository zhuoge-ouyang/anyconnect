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
	"time"
)

const endpoint = "https://chatgpt.com/backend-api/codex/responses"

type Result struct {
	Status      int
	Duration    time.Duration
	CFRay       string
	CFMitigated string
	Error       string
}

func (r Result) Healthy() bool {
	return r.Error == "" &&
		r.Status == http.StatusUnauthorized &&
		!strings.EqualFold(r.CFMitigated, "challenge")
}

func (r Result) Blocked() bool {
	return r.Status == http.StatusForbidden || strings.EqualFold(r.CFMitigated, "challenge")
}

func Probe(ctx context.Context) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return Result{Error: err.Error()}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "anyconnect-split-codex-probe/1.0")

	start := time.Now()
	resp, err := client().Do(req)
	duration := time.Since(start)
	if err != nil {
		return Result{Duration: duration, Error: err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2048))

	return Result{
		Status:      resp.StatusCode,
		Duration:    duration,
		CFRay:       resp.Header.Get("CF-RAY"),
		CFMitigated: resp.Header.Get("Cf-Mitigated"),
	}
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
