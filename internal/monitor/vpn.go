package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
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

type VPNPresence int

const (
	PresenceUnknown VPNPresence = iota
	PresenceConnected
	PresenceDisconnected
)

func (p VPNPresence) String() string {
	switch p {
	case PresenceConnected:
		return "Connected"
	case PresenceDisconnected:
		return "Disconnected"
	default:
		return "Unknown"
	}
}

type StateChange struct {
	OldState VPNState
	NewState VPNState
}

type Detector func() VPNPresence

type DefaultRoute struct {
	Gateway        string
	InterfaceIP    string
	InterfaceIndex int
}

func isCiscoAdapter(name string) bool {
	return isCiscoText(name)
}

func isCiscoText(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "cisco") || strings.Contains(lower, "anyconnect")
}

func runPowerShell(script string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return string(output), err
}

func hasCiscoAdapterUpViaPowerShell() bool {
	script := `Get-NetAdapter -IncludeHidden | Where-Object { $_.Status -eq 'Up' -and ($_.InterfaceDescription -match 'Cisco|AnyConnect' -or $_.Name -match 'Cisco|AnyConnect') } | Select-Object -First 1 -ExpandProperty ifIndex`
	output, err := runPowerShell(script, 3*time.Second)
	return err == nil && strings.TrimSpace(output) != ""
}

func isCiscoInterfaceIndexViaPowerShell(index int) bool {
	if index <= 0 {
		return false
	}
	script := fmt.Sprintf(`Get-NetAdapter -IncludeHidden | Where-Object { $_.ifIndex -eq %d -and ($_.InterfaceDescription -match 'Cisco|AnyConnect' -or $_.Name -match 'Cisco|AnyConnect') } | Select-Object -First 1 -ExpandProperty ifIndex`, index)
	output, err := runPowerShell(script, 3*time.Second)
	return err == nil && strings.TrimSpace(output) != ""
}

func HasActiveCiscoAdapter() bool {
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 {
				continue
			}
			if isCiscoAdapter(iface.Name) {
				return true
			}
		}
	} else {
		log.Printf("Error listing interfaces: %v", err)
	}
	return hasCiscoAdapterUpViaPowerShell()
}

func hasCiscoDefaultRouteInNetshOutput(output string, isCiscoIndex func(int) bool) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field != "0.0.0.0/0" || i+1 >= len(fields) {
				continue
			}
			index, err := strconv.Atoi(fields[i+1])
			if err == nil && isCiscoIndex(index) {
				return true
			}
		}
	}
	return false
}

func HasCiscoDefaultRoute() bool {
	script := `$routes = Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue; foreach ($route in $routes) { $adapter = Get-NetAdapter -IncludeHidden -InterfaceIndex $route.InterfaceIndex -ErrorAction SilentlyContinue; if ($adapter -and ($adapter.InterfaceDescription -match 'Cisco|AnyConnect' -or $adapter.Name -match 'Cisco|AnyConnect')) { $route.InterfaceIndex; break } }`
	output, err := runPowerShell(script, 8*time.Second)
	return err == nil && strings.TrimSpace(output) != ""
}

type Monitor struct {
	mu                  sync.Mutex
	state               VPNState
	onChange            chan StateChange
	stop                chan struct{}
	stopOnce            sync.Once
	interval            time.Duration
	detect              Detector
	disconnectedChecks  int
	disconnectThreshold int
}

func New() *Monitor {
	return NewWithDetector(nil)
}

func NewWithDetector(detect func() bool) *Monitor {
	var detector Detector
	if detect == nil {
		detector = defaultDetectVPN
	} else {
		detector = func() VPNPresence {
			if detect() {
				return PresenceConnected
			}
			return PresenceDisconnected
		}
	}
	return NewWithStatusDetector(detector)
}

func NewWithStatusDetector(detect Detector) *Monitor {
	if detect == nil {
		detect = defaultDetectVPN
	}
	return &Monitor{
		state:               StateIdle,
		onChange:            make(chan StateChange, 10),
		stop:                make(chan struct{}),
		interval:            5 * time.Second,
		detect:              detect,
		disconnectThreshold: 24,
	}
}

func (m *Monitor) StateChanges() <-chan StateChange {
	return m.onChange
}

func (m *Monitor) State() VPNState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *Monitor) SetState(s VPNState) {
	m.mu.Lock()
	old := m.state
	m.state = s
	m.mu.Unlock()
	if old != s {
		m.onChange <- StateChange{OldState: old, NewState: s}
	}
}

func (m *Monitor) Start() {
	go m.pollLoop()
}

func (m *Monitor) Stop() {
	m.stopOnce.Do(func() {
		close(m.stop)
	})
}

func (m *Monitor) pollLoop() {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.handleDetection(m.detect())
		}
	}
}

func (m *Monitor) handleDetection(presence VPNPresence) {
	switch m.State() {
	case StateIdle:
		if presence == PresenceConnected {
			m.resetDisconnectedChecks()
			m.SetState(StateConnected)
		}
	case StateActive:
		switch presence {
		case PresenceConnected:
			m.resetDisconnectedChecks()
		case PresenceDisconnected:
			if m.markDisconnectedCheck() {
				m.SetState(StateCleaning)
			}
		case PresenceUnknown:
			m.resetDisconnectedChecks()
			log.Println("VPN monitor status is unknown; preserving active routes")
		}
	}
}

func (m *Monitor) resetDisconnectedChecks() {
	m.mu.Lock()
	m.disconnectedChecks = 0
	m.mu.Unlock()
}

func (m *Monitor) markDisconnectedCheck() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disconnectedChecks++
	log.Printf("VPN monitor did not see an active VPN (%d/%d)", m.disconnectedChecks, m.disconnectThreshold)
	return m.disconnectedChecks >= m.disconnectThreshold
}

func defaultDetectVPN() VPNPresence {
	if HasActiveCiscoAdapter() || HasCiscoDefaultRoute() {
		return PresenceConnected
	}
	return PresenceDisconnected
}

func isCiscoInterfaceIndex(index int) bool {
	if index <= 0 {
		return false
	}
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Index == index {
				if isCiscoAdapter(iface.Name) {
					return true
				}
				break
			}
		}
	}
	return isCiscoInterfaceIndexViaPowerShell(index)
}

func GetDefaultGateway() (string, error) {
	route, err := GetDefaultRoute()
	if err != nil {
		return "", err
	}
	return route.Gateway, nil
}

func GetDefaultIPv6Route() (DefaultRoute, error) {
	script := `Get-NetRoute -AddressFamily IPv6 -DestinationPrefix '::/0' | Where-Object { $_.NextHop -and $_.NextHop -ne '::' } | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1 InterfaceIndex,NextHop | ConvertTo-Json -Compress`
	output, err := runPowerShell(script, 8*time.Second)
	if err != nil {
		return DefaultRoute{}, fmt.Errorf("query IPv6 default route failed: %w", err)
	}
	text := strings.TrimSpace(output)
	if text == "" || text == "null" {
		return DefaultRoute{}, fmt.Errorf("IPv6 default route not found")
	}
	var route struct {
		InterfaceIndex int
		NextHop        string
	}
	if err := json.Unmarshal([]byte(text), &route); err != nil {
		return DefaultRoute{}, fmt.Errorf("parse IPv6 default route: %w", err)
	}
	if route.InterfaceIndex <= 0 || route.NextHop == "" || route.NextHop == "::" {
		return DefaultRoute{}, fmt.Errorf("IPv6 default route not found")
	}
	if isCiscoInterfaceIndex(route.InterfaceIndex) {
		return DefaultRoute{}, fmt.Errorf("IPv6 default route is Cisco adapter")
	}
	return DefaultRoute{
		Gateway:        route.NextHop,
		InterfaceIndex: route.InterfaceIndex,
	}, nil
}

func GetDefaultRoute() (DefaultRoute, error) {
	cmd := exec.Command("route", "print", "0.0.0.0")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	output, err := cmd.Output()
	if err != nil {
		return DefaultRoute{}, fmt.Errorf("route print failed: %w", err)
	}
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 5 && fields[0] == "0.0.0.0" && fields[1] == "0.0.0.0" {
			gateway := fields[2]
			if gateway != "0.0.0.0" && !strings.EqualFold(gateway, "on-link") {
				ifaceIP := fields[3]
				index := interfaceIndexByIP(ifaceIP)
				if isCiscoInterfaceIndex(index) {
					continue
				}
				return DefaultRoute{
					Gateway:        gateway,
					InterfaceIP:    ifaceIP,
					InterfaceIndex: index,
				}, nil
			}
		}
	}
	return DefaultRoute{}, fmt.Errorf("default gateway not found")
}

func interfaceIndexByIP(ip string) int {
	target := net.ParseIP(ip)
	if target == nil {
		return 0
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return 0
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var addrIP net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				addrIP = v.IP
			case *net.IPAddr:
				addrIP = v.IP
			}
			if addrIP != nil && addrIP.Equal(target) {
				return iface.Index
			}
		}
	}
	return 0
}
