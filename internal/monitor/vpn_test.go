package monitor

import "testing"

func TestIsCiscoAdapter(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Cisco AnyConnect Secure Mobility Client Virtual Miniport Adapter", true},
		{"Cisco AnyConnect Virtual Adapter", true},
		{"cisco anyconnect", true},
		{"Intel(R) Wi-Fi 6 AX201 160MHz", false},
		{"Realtek PCIe GbE Family Controller", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isCiscoAdapter(tt.name)
		if got != tt.want {
			t.Errorf("isCiscoAdapter(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
