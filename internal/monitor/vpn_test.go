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

func TestMonitorUnknownDetectionDoesNotCleanActiveRoutes(t *testing.T) {
	m := NewWithStatusDetector(nil)
	m.state = StateActive
	m.disconnectThreshold = 1

	m.handleDetection(PresenceUnknown)

	if got := m.State(); got != StateActive {
		t.Fatalf("state after unknown detection = %s, want %s", got, StateActive)
	}
	if m.disconnectedChecks != 0 {
		t.Fatalf("disconnectedChecks after unknown detection = %d, want 0", m.disconnectedChecks)
	}
}

func TestMonitorDisconnectedDetectionCanCleanActiveRoutes(t *testing.T) {
	m := NewWithStatusDetector(nil)
	m.state = StateActive
	m.disconnectThreshold = 2

	m.handleDetection(PresenceDisconnected)
	if got := m.State(); got != StateActive {
		t.Fatalf("state after first disconnected detection = %s, want %s", got, StateActive)
	}

	m.handleDetection(PresenceDisconnected)
	if got := m.State(); got != StateCleaning {
		t.Fatalf("state after threshold disconnected detections = %s, want %s", got, StateCleaning)
	}
}

func TestMonitorDefaultDisconnectDetectionCleansImmediately(t *testing.T) {
	m := NewWithStatusDetector(nil)
	m.state = StateActive

	m.handleDetection(PresenceDisconnected)

	if got := m.State(); got != StateCleaning {
		t.Fatalf("state after definitive disconnect = %s, want %s", got, StateCleaning)
	}
}

func TestHasCiscoDefaultRouteInNetshOutput(t *testing.T) {
	output := `
Publish  Type      Met  Prefix                    Idx  Gateway/Interface Name
-------  --------  ---  ------------------------  ---  ------------------------
No       Manual    0    0.0.0.0/0                  21  192.168.3.1
No       Manual    1    0.0.0.0/0                  11  10.21.0.1
`
	isCisco := func(index int) bool {
		return index == 11
	}
	if !hasCiscoDefaultRouteInNetshOutput(output, isCisco) {
		t.Fatal("hasCiscoDefaultRouteInNetshOutput() did not find Cisco default route")
	}
}

func TestHasCiscoDefaultRouteInNetshOutputRejectsLocalOnly(t *testing.T) {
	output := `
Publish  Type      Met  Prefix                    Idx  Gateway/Interface Name
-------  --------  ---  ------------------------  ---  ------------------------
No       Manual    0    0.0.0.0/0                  21  192.168.3.1
`
	isCisco := func(index int) bool {
		return index == 11
	}
	if hasCiscoDefaultRouteInNetshOutput(output, isCisco) {
		t.Fatal("hasCiscoDefaultRouteInNetshOutput() found Cisco route in local-only table")
	}
}
