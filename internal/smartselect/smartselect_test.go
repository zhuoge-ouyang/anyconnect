package smartselect

import (
	"path/filepath"
	"testing"
	"time"
)

func healthyRounds(n int, latency time.Duration, ip string) []Round {
	out := make([]Round, n)
	for i := range out {
		out[i] = Round{true, true, latency, false, ip, "JP", "vpn-direct"}
	}
	return out
}

func TestTransactionRoundTripAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transaction.json")
	want := Transaction{Pending: true, OriginalSite: "22.日本", OriginalCodexMode: true, StartedAt: time.Now().Truncate(time.Second)}
	if err := SaveTransaction(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTransaction(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Pending || got.OriginalSite != want.OriginalSite || got.OriginalCodexMode != want.OriginalCodexMode {
		t.Fatalf("got=%+v", got)
	}
	if err := ClearTransaction(path); err != nil {
		t.Fatal(err)
	}
	got, err = LoadTransaction(path)
	if err != nil || got.Pending {
		t.Fatalf("after clear got=%+v err=%v", got, err)
	}
}

func TestStrictQualification(t *testing.T) {
	if !CurrentHealthy(Evaluate(healthyRounds(3, 900*time.Millisecond, "1.1.1.1"))) {
		t.Fatal("3/3 current should pass")
	}
	if CandidateQualified(Evaluate(healthyRounds(3, 900*time.Millisecond, "1.1.1.1"))) {
		t.Fatal("candidate requires 4/4")
	}
	r := healthyRounds(4, 900*time.Millisecond, "1.1.1.1")
	r[2].APIHealthy = false
	if CandidateQualified(Evaluate(r)) {
		t.Fatal("a single endpoint failure must reject")
	}
}

func TestLatencyLimitsAndImprovement(t *testing.T) {
	if CandidateQualified(Evaluate(healthyRounds(4, 2600*time.Millisecond, "1.1.1.1"))) {
		t.Fatal("median limit not enforced")
	}
	current := Evaluate(healthyRounds(3, 2*time.Second, "1.1.1.1"))
	if !MateriallyBetter(current, Evaluate(healthyRounds(4, 1400*time.Millisecond, "2.2.2.2"))) {
		t.Fatal("30 percent and 600ms should pass")
	}
	if MateriallyBetter(current, Evaluate(healthyRounds(4, 1600*time.Millisecond, "2.2.2.2"))) {
		t.Fatal("20 percent must not pass")
	}
}

func TestCooldownAndRanking(t *testing.T) {
	now := time.Now()
	h := History{Version: 1, Sites: map[string]Record{
		"bad":  {Site: "bad", ConsecutiveFailures: 1, LastFailure: now},
		"good": {Site: "good", Successes: 4, MedianMS: 600, LastSuccess: now},
	}}
	got := Rank([]string{"bad", "new", "good", "current"}, "current", h, now)
	if len(got) != 2 || got[0] != "good" || got[1] != "new" {
		t.Fatalf("rank=%v", got)
	}
}
