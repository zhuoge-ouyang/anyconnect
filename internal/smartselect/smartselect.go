package smartselect

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	Deadline          = 60 * time.Second
	DecisionCountdown = 20 * time.Second
	Cooldown          = 30 * time.Minute
	MaxCandidates     = 2
	CurrentRounds     = 3
	CandidateRounds   = 4
	MedianLimit       = 2500 * time.Millisecond
	SlowestLimit      = 5 * time.Second
	MinImprovement    = 300 * time.Millisecond
)

type Round struct {
	ChatGPTHealthy bool
	APIHealthy     bool
	Duration       time.Duration
	Blocked        bool
	ExitIP         string
	ExitRegion     string
	Route          string
}

type Metrics struct {
	Attempts   int
	Successes  int
	Median     time.Duration
	Slowest    time.Duration
	ExitIP     string
	ExitRegion string
}

func Evaluate(rounds []Round) Metrics {
	m := Metrics{Attempts: len(rounds)}
	var durations []time.Duration
	for _, r := range rounds {
		if r.ChatGPTHealthy && r.APIHealthy && !r.Blocked && r.Route == "vpn-direct" && r.ExitIP != "" {
			m.Successes++
			durations = append(durations, r.Duration)
			m.ExitIP, m.ExitRegion = r.ExitIP, r.ExitRegion
		}
	}
	if len(durations) == 0 {
		return m
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	m.Median = durations[len(durations)/2]
	m.Slowest = durations[len(durations)-1]
	return m
}

func CurrentHealthy(m Metrics) bool     { return qualified(m, CurrentRounds) }
func CandidateQualified(m Metrics) bool { return qualified(m, CandidateRounds) }

func qualified(m Metrics, rounds int) bool {
	return m.Attempts == rounds && m.Successes == rounds && m.Median > 0 &&
		m.Median <= MedianLimit && m.Slowest <= SlowestLimit && m.ExitIP != ""
}

func MateriallyBetter(current, candidate Metrics) bool {
	if current.Median <= 0 || candidate.Median <= 0 || candidate.Successes < current.Successes {
		return false
	}
	return current.Median-candidate.Median >= MinImprovement &&
		float64(candidate.Median) <= float64(current.Median)*0.75
}

type Record struct {
	Site                string    `json:"site"`
	LastSuccess         time.Time `json:"last_success,omitempty"`
	LastFailure         time.Time `json:"last_failure,omitempty"`
	Successes           int       `json:"successes"`
	Failures            int       `json:"failures"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	MedianMS            int64     `json:"median_ms,omitempty"`
	SlowestMS           int64     `json:"slowest_ms,omitempty"`
	LastSelected        time.Time `json:"last_selected,omitempty"`
}

type History struct {
	Version int               `json:"version"`
	Sites   map[string]Record `json:"sites"`
}

func LoadHistory(path string) (History, error) {
	h := History{Version: 1, Sites: map[string]Record{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return h, err
	}
	if err := json.Unmarshal(data, &h); err != nil {
		return h, err
	}
	if h.Sites == nil {
		h.Sites = map[string]Record{}
	}
	return h, nil
}

func (h *History) Record(site string, m Metrics, success bool, now time.Time) {
	r := h.Sites[site]
	r.Site = site
	if success {
		r.Successes++
		r.ConsecutiveFailures = 0
		r.LastSuccess = now
		r.MedianMS, r.SlowestMS = m.Median.Milliseconds(), m.Slowest.Milliseconds()
	} else {
		r.Failures++
		r.ConsecutiveFailures++
		r.LastFailure = now
	}
	h.Sites[site] = r
}

func (h History) InCooldown(site string, now time.Time) bool {
	r, ok := h.Sites[site]
	return ok && r.ConsecutiveFailures > 0 && now.Sub(r.LastFailure) < Cooldown
}

func (h History) Save(path string) error { return writeJSON(path, h) }

type Transaction struct {
	Pending           bool      `json:"pending"`
	OriginalSite      string    `json:"original_site"`
	OriginalCodexMode bool      `json:"original_codex_mode"`
	StartedAt         time.Time `json:"started_at"`
}

func LoadTransaction(path string) (Transaction, error) {
	var tx Transaction
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return tx, nil
	}
	if err != nil {
		return tx, err
	}
	err = json.Unmarshal(data, &tx)
	return tx, err
}
func SaveTransaction(path string, tx Transaction) error { return writeJSON(path, tx) }
func ClearTransaction(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func Rank(names []string, current string, h History, now time.Time) []string {
	items := append([]string(nil), names...)
	sort.SliceStable(items, func(i, j int) bool {
		a, b := h.Sites[items[i]], h.Sites[items[j]]
		ac, bc := h.InCooldown(items[i], now), h.InCooldown(items[j], now)
		if ac != bc {
			return !ac
		}
		ar, br := rate(a), rate(b)
		if ar != br {
			return ar > br
		}
		if !a.LastSuccess.Equal(b.LastSuccess) {
			return a.LastSuccess.After(b.LastSuccess)
		}
		return a.MedianMS < b.MedianMS
	})
	eligible := make([]string, 0, len(items))
	for _, name := range items {
		if name == current || h.InCooldown(name, now) {
			continue
		}
		eligible = append(eligible, name)
	}
	if len(eligible) <= MaxCandidates {
		return eligible
	}
	out := []string{eligible[0]}
	for _, name := range eligible[1:] {
		if Region(name) != Region(out[0]) {
			out = append(out, name)
			return out
		}
	}
	out = append(out, eligible[1])
	return out
}

func rate(r Record) float64 {
	total := r.Successes + r.Failures
	if total == 0 {
		return 0
	}
	return float64(r.Successes) / float64(total)
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Region(site string) string {
	if i := strings.Index(site, "."); i >= 0 {
		site = site[i+1:]
	}
	return strings.TrimSpace(site)
}
