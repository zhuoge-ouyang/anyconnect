package main

import (
	"strings"
	"testing"

	"github.com/user/anyconnect-split/internal/config"
)

func TestSummarizeAndValidateRoutePlanAllowsSmallPlan(t *testing.T) {
	got, err := summarizeAndValidateRoutePlan("domestic whitelist", []string{
		"203.0.113.8/32",
		"203.0.113.8/32",
		"198.51.100.0/24",
	}, 10)
	if err != nil {
		t.Fatalf("summarizeAndValidateRoutePlan returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(summarized) = %d, want 2", len(got))
	}
}

func TestSummarizeAndValidateRoutePlanRejectsOversizedPlan(t *testing.T) {
	_, err := summarizeAndValidateRoutePlan("foreign whitelist", []string{
		"203.0.113.1/32",
		"198.51.100.1/32",
		"192.0.2.1/32",
	}, 2)
	if err == nil {
		t.Fatal("summarizeAndValidateRoutePlan should reject plans over the route limit")
	}
	if !strings.Contains(err.Error(), "foreign whitelist") || !strings.Contains(err.Error(), "3") || !strings.Contains(err.Error(), "2") {
		t.Fatalf("error = %v, want label and route counts", err)
	}
}

func TestEffectiveRouteEntryLimitUsesDefaultForInvalidLimit(t *testing.T) {
	if got := effectiveRouteEntryLimit(0); got != config.DefaultRouteEntryLimit {
		t.Fatalf("effectiveRouteEntryLimit(0) = %d, want default %d", got, config.DefaultRouteEntryLimit)
	}
}
