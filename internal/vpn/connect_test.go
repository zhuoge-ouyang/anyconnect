package vpn

import (
	"testing"

	"github.com/user/anyconnect-split/internal/monitor"
)

func TestStatusPresenceFromOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   monitor.VPNPresence
		ok     bool
	}{
		{
			name:   "connected",
			output: "  >> state: Connected\nVPN>",
			want:   monitor.PresenceConnected,
			ok:     true,
		},
		{
			name:   "reconnecting preserves routes",
			output: "  >> state: Reconnecting\n  >> notice: Reconnecting to ...",
			want:   monitor.PresenceConnected,
			ok:     true,
		},
		{
			name:   "disconnected",
			output: "  >> state: Disconnected\n  >> notice: Ready to connect.",
			want:   monitor.PresenceDisconnected,
			ok:     true,
		},
		{
			name:   "banner only is unknown",
			output: "Cisco AnyConnect Secure Mobility Client (version 4.10.04071).",
			want:   monitor.PresenceUnknown,
			ok:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := statusPresenceFromOutput(tt.output)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("presence = %s, want %s", got, tt.want)
			}
		})
	}
}
