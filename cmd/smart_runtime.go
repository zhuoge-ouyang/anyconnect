package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/user/anyconnect-split/internal/codexprobe"
	"github.com/user/anyconnect-split/internal/dashboard"
	"github.com/user/anyconnect-split/internal/smartselect"
	"github.com/user/anyconnect-split/internal/ui"
)

type smartRuntime struct {
	mu       sync.Mutex
	state    dashboard.Snapshot
	cancel   context.CancelFunc
	decision chan string
	running  bool
}

func prefilterSites(ctx context.Context, sites []ui.Site) []ui.Site {
	type measured struct {
		site    ui.Site
		latency time.Duration
		ok      bool
		index   int
	}
	results := make(chan measured, len(sites))
	for i, site := range sites {
		go func(index int, candidate ui.Site) {
			start := time.Now()
			u, err := url.Parse(candidate.Server)
			address := ""
			if err == nil {
				address = u.Host
			}
			if address != "" && u.Port() == "" {
				address = net.JoinHostPort(u.Hostname(), "443")
			}
			if address == "" {
				results <- measured{site: candidate, index: index}
				return
			}
			dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", address)
			if err == nil {
				_ = conn.Close()
			}
			results <- measured{site: candidate, latency: time.Since(start), ok: err == nil, index: index}
		}(i, site)
	}
	measuredSites := make([]measured, 0, len(sites))
	for range sites {
		measuredSites = append(measuredSites, <-results)
	}
	sort.SliceStable(measuredSites, func(i, j int) bool {
		if measuredSites[i].ok != measuredSites[j].ok {
			return measuredSites[i].ok
		}
		if measuredSites[i].ok && measuredSites[i].latency != measuredSites[j].latency {
			return measuredSites[i].latency < measuredSites[j].latency
		}
		return measuredSites[i].index < measuredSites[j].index
	})
	out := make([]ui.Site, 0, len(sites))
	for _, item := range measuredSites {
		out = append(out, item.site)
	}
	return out
}

func runSmartProbes(ctx context.Context, rounds int, onRound func(int)) []smartselect.Round {
	results := make([]smartselect.Round, 0, rounds)
	for i := 0; i < rounds; i++ {
		if ctx.Err() != nil {
			break
		}
		probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		result := codexprobe.ProbeServices(probeCtx)
		cancel()
		results = append(results, smartselect.Round{
			ChatGPTHealthy: result.ChatGPT.Healthy(), APIHealthy: result.OpenAIAPI.Healthy(),
			Duration: result.Duration, Blocked: result.Blocked(), ExitIP: result.ExitIP,
			ExitRegion: result.Region, Route: "vpn-direct",
		})
		if onRound != nil {
			onRound(i + 1)
		}
	}
	return results
}

func newSmartRuntime() *smartRuntime { return &smartRuntime{} }

func (r *smartRuntime) begin(parent context.Context) (context.Context, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(parent, smartselect.Deadline)
	r.running, r.cancel = true, cancel
	r.decision = make(chan string, 2)
	r.state = dashboard.Snapshot{SmartState: "running", SmartMessage: "正在复测当前线路", SmartDeadline: time.Now().Add(smartselect.Deadline)}
	return ctx, true
}

func (r *smartRuntime) update(state, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.SmartState, r.state.SmartMessage = state, message
}

func (r *smartRuntime) result(state, site string, m smartselect.Metrics) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.SmartState = state
	r.state.SmartResultID = fmt.Sprintf("%d", time.Now().UnixNano())
	r.state.SmartCandidate = site
	r.state.SmartAttempts, r.state.SmartSuccesses = m.Attempts, m.Successes
	r.state.SmartMedianMS, r.state.SmartSlowestMS = m.Median.Milliseconds(), m.Slowest.Milliseconds()
	r.state.SmartExitIP, r.state.SmartExitRegion = m.ExitIP, m.ExitRegion
}

func (r *smartRuntime) snapshot() dashboard.Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}
func (r *smartRuntime) isRunning() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.running }
func (r *smartRuntime) choose(v string) {
	r.mu.Lock()
	ch := r.decision
	r.mu.Unlock()
	if ch != nil {
		select {
		case ch <- v:
		default:
		}
	}
}
func (r *smartRuntime) decisions() <-chan string { r.mu.Lock(); defer r.mu.Unlock(); return r.decision }
func (r *smartRuntime) stop() {
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()
}
func (r *smartRuntime) finish(message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
	r.running, r.cancel, r.decision = false, nil, nil
	r.state.SmartState, r.state.SmartMessage = "idle", message
	r.state.SmartDeadline = time.Time{}
}
