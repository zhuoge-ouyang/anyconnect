package ui

import (
	"strings"
	"testing"
)

func TestDashboardScriptContainsSplitModeControl(t *testing.T) {
	script := dashboardScript(
		`C:\Temp\anyconnect-dashboard-state.json`,
		`C:\Temp\anyconnect-dashboard-commands`,
		`C:\Temp\app.ico`,
	)

	for _, want := range []string{"分流已常驻", "国内直连优先", "国外 VPN 优先", "set_split_mode", "split_mode"} {
		if !strings.Contains(script, want) {
			t.Fatalf("dashboard script missing %q", want)
		}
	}
}

func TestDashboardScriptDoesNotExposeSplitDisableControl(t *testing.T) {
	script := dashboardScript(
		`C:\Temp\anyconnect-dashboard-state.json`,
		`C:\Temp\anyconnect-dashboard-commands`,
		`C:\Temp\app.ico`,
	)

	for _, forbidden := range []string{"$chkSplit", "Write-Command 'toggle_split'"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("dashboard script still exposes split disable control %q", forbidden)
		}
	}
}

func TestDashboardScriptContainsSmartSelectionAndWhitelistDialog(t *testing.T) {
	script := dashboardScript(`C:\Temp\state.json`, `C:\Temp\commands`, `C:\Temp\app.ico`)
	for _, want := range []string{
		"ChatGPT/Codex 智能选线", "最多 60 秒", "Show-RecommendationDialog",
		"20 秒后自动采用推荐线路", "$dialog.ControlBox = $false",
		"管理国外白名单", "remove_foreign_domain", "remove_foreign_cidr",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("dashboard script missing %q", want)
		}
	}
	for _, clipped := range []string{"$txtDomain.Location", "$txtIP.Location"} {
		if strings.Contains(script, clipped) {
			t.Fatalf("dashboard still contains clipped inline control %q", clipped)
		}
	}
}
