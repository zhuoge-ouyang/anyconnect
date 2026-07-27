package tray

import (
	"testing"

	"github.com/user/anyconnect-split/internal/config"
)

func TestResolveIconClickActionSeparatesLeftAndRight(t *testing.T) {
	tests := []struct {
		name       string
		button     IconButton
		hasHandler bool
		want       IconClickAction
	}{
		{name: "left click opens dashboard when handler exists", button: IconButtonLeft, hasHandler: true, want: IconClickOpenDashboard},
		{name: "left click falls back to menu without handler", button: IconButtonLeft, hasHandler: false, want: IconClickShowMenu},
		{name: "right click stays on native menu path", button: IconButtonRight, hasHandler: true, want: IconClickIgnore},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveIconClickAction(tt.button, tt.hasHandler); got != tt.want {
				t.Fatalf("ResolveIconClickAction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIconButtonFromClickUsesNativeButton(t *testing.T) {
	if got := iconButtonFromClick(true); got != IconButtonLeft {
		t.Fatalf("left click button = %v, want %v", got, IconButtonLeft)
	}
	if got := iconButtonFromClick(false); got != IconButtonRight {
		t.Fatalf("right click button = %v, want %v", got, IconButtonRight)
	}
}

func TestTrayStatusListenerReceivesStatusChanges(t *testing.T) {
	tray := New(Actions{}, false)
	var got Status
	tray.SetStatusListener(func(status Status) {
		got = status
	})

	tray.SetCurrentSite("03.国内专线-深圳节点")
	tray.SetStatusFields("状态：TUN 分流已启用", 42, "")

	if got.CurrentSite != "03.国内专线-深圳节点" {
		t.Fatalf("listener current site = %q", got.CurrentSite)
	}
	if got.StatusText != "状态：TUN 分流已启用" || got.RouteCount != 42 {
		t.Fatalf("listener status = %#v", got)
	}
	if got.CodexModeActive {
		t.Fatalf("listener codex mode active = true by default")
	}
	if !got.SplitEnabled {
		t.Fatalf("listener split enabled = false, want true")
	}

	tray.SetCodexModeActive(true)
	if !got.CodexModeActive {
		t.Fatalf("listener codex mode active = false after SetCodexModeActive(true)")
	}

}

func TestModeActionVisibilityIsMutuallyExclusive(t *testing.T) {
	showCodex, showRestore := modeActionVisibility(false)
	if !showCodex || showRestore {
		t.Fatalf("normal visibility = codex:%v restore:%v, want only codex", showCodex, showRestore)
	}
	showCodex, showRestore = modeActionVisibility(true)
	if showCodex || !showRestore {
		t.Fatalf("codex visibility = codex:%v restore:%v, want only restore", showCodex, showRestore)
	}
}

func TestTraySplitModeStateAndRequest(t *testing.T) {
	calls := 0
	requested := ""
	tray := New(Actions{OnSetSplitMode: func(mode string) {
		calls++
		requested = mode
	}}, false)
	var got Status
	tray.SetStatusListener(func(status Status) { got = status })

	tray.SetSplitMode(config.SplitModeDomesticDirect)
	tray.requestSplitMode(config.SplitModeDomesticDirect)
	if calls != 0 {
		t.Fatalf("same mode dispatched %d calls, want 0", calls)
	}

	tray.requestSplitMode(config.SplitModeForeignDirect)
	if calls != 1 || requested != config.SplitModeForeignDirect {
		t.Fatalf("mode request calls=%d mode=%q", calls, requested)
	}
	if got.SplitMode != config.SplitModeDomesticDirect {
		t.Fatalf("request changed status before persistence: %q", got.SplitMode)
	}

	tray.SetSplitMode(config.SplitModeForeignDirect)
	if got.SplitMode != config.SplitModeForeignDirect {
		t.Fatalf("status split mode = %q, want foreign_direct", got.SplitMode)
	}
}

func TestSplitModeChecksAreMutuallyExclusive(t *testing.T) {
	domestic, foreign := splitModeChecks(config.SplitModeDomesticDirect)
	if !domestic || foreign {
		t.Fatalf("domestic checks = %v/%v, want true/false", domestic, foreign)
	}
	domestic, foreign = splitModeChecks(config.SplitModeForeignDirect)
	if domestic || !foreign {
		t.Fatalf("foreign checks = %v/%v, want false/true", domestic, foreign)
	}
}
