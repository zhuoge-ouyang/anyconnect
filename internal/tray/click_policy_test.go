package tray

import "testing"

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

func TestTrayStatusListenerReceivesStatusChanges(t *testing.T) {
	tray := New(Actions{}, true, false)
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
	if !got.SplitEnabled {
		t.Fatalf("listener split enabled = false, want true")
	}

	tray.SetSplitEnabled(false)
	if got.SplitEnabled {
		t.Fatalf("listener split enabled = true after SetSplitEnabled(false)")
	}
}
