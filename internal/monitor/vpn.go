package monitor

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
	"time"
)

type VPNState int

const (
	StateIdle      VPNState = iota // VPN not connected
	StateConnected                 // VPN connected, routes pending
	StateActive                    // Routes applied
	StateCleaning                  // Cleaning routes
)

func (s VPNState) String() string {
	switch s {
	case StateIdle:
		return "Idle"
	case StateConnected:
		return "Connected"
	case StateActive:
		return "Active"
	case StateCleaning:
		return "Cleaning"
	default:
		return "Unknown"
	}
}

type StateChange struct {
	OldState VPNState
	NewState VPNState
}

func isCiscoAdapter(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "cisco")
}

type Monitor struct {
	state    VPNState
	onChange chan StateChange
	stop     chan struct{}
	interval time.Duration
}

func New() *Monitor {
	return &Monitor{
		state:    StateIdle,
		onChange: make(chan StateChange, 10),
		stop:     make(chan struct{}),
		interval: 2 * time.Second,
	}
}

func (m *Monitor) StateChanges() <-chan StateChange {
	return m.onChange
}

func (m *Monitor) State() VPNState {
	return m.state
}

func (m *Monitor) SetState(s VPNState) {
	old := m.state
	m.state = s
	if old != s {
		m.onChange <- StateChange{OldState: old, NewState: s}
	}
}

func (m *Monitor) Start() {
	go m.pollLoop()
}

func (m *Monitor) Stop() {
	close(m.stop)
}

func (m *Monitor) pollLoop() {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			vpnUp := m.detectVPN()
			switch m.state {
			case StateIdle:
				if vpnUp {
					m.SetState(StateConnected)
				}
			case StateActive:
				if !vpnUp {
					m.SetState(StateCleaning)
				}
			}
		}
	}
}

func (m *Monitor) detectVPN() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Printf("Error listing interfaces: %v", err)
		return false
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if isCiscoAdapter(iface.Name) {
			return true
		}
	}
	return false
}

func GetDefaultGateway() (string, error) {
	cmd := exec.Command("route", "print", "0.0.0.0")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("route print failed: %w", err)
	}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[0] == "0.0.0.0" && fields[1] == "0.0.0.0" {
			gateway := fields[2]
			if gateway != "0.0.0.0" && !strings.HasPrefix(gateway, "10.") {
				return gateway, nil
			}
		}
	}
	return "", fmt.Errorf("default gateway not found")
}
