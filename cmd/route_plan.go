package main

import (
	"fmt"

	"github.com/user/anyconnect-split/internal/config"
	"github.com/user/anyconnect-split/internal/route"
)

func effectiveRouteEntryLimit(limit int) int {
	if limit <= 0 {
		return config.DefaultRouteEntryLimit
	}
	return limit
}

func summarizeAndValidateRoutePlan(label string, cidrs []string, limit int) ([]string, error) {
	summarized := route.SummarizeCIDRs(cidrs)
	effectiveLimit := effectiveRouteEntryLimit(limit)
	if len(summarized) > effectiveLimit {
		return nil, fmt.Errorf("%s route plan has %d entries, exceeds limit %d", label, len(summarized), effectiveLimit)
	}
	return summarized, nil
}
