package codexprobe

import (
	"net/http"
	"testing"
)

func TestResultHealthyRequiresExpectedUnauthorizedWithoutChallenge(t *testing.T) {
	if !((Result{Status: http.StatusUnauthorized}).Healthy()) {
		t.Fatal("401 should be healthy")
	}
	if (Result{Status: http.StatusForbidden}).Healthy() {
		t.Fatal("403 must not be healthy")
	}
	if (Result{Status: http.StatusUnauthorized, CFMitigated: "challenge"}).Healthy() {
		t.Fatal("challenge must not be healthy")
	}
}

func TestParseTrace(t *testing.T) {
	ip, region := parseTrace("fl=1\nip=203.0.113.7\ncolo=SIN\n")
	if ip != "203.0.113.7" || region != "SIN" {
		t.Fatalf("trace=%q/%q", ip, region)
	}
}
