package route

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

func prefixToMask(prefix int) string {
	mask := net.CIDRMask(prefix, 32)
	return fmt.Sprintf("%d.%d.%d.%d", mask[0], mask[1], mask[2], mask[3])
}

func parseCIDR(cidr string) (network string, prefix int, err error) {
	parts := strings.Split(cidr, "/")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid CIDR: %s", cidr)
	}
	var p int
	fmt.Sscanf(parts[1], "%d", &p)
	return parts[0], p, nil
}

func buildAddCommand(cidr, gateway string) string {
	network, prefix, _ := parseCIDR(cidr)
	mask := prefixToMask(prefix)
	return fmt.Sprintf("route add %s mask %s %s metric 5", network, mask, gateway)
}

func buildDeleteCommand(cidr string) string {
	network, prefix, _ := parseCIDR(cidr)
	mask := prefixToMask(prefix)
	return fmt.Sprintf("route delete %s mask %s", network, mask)
}

type Manager struct {
	gateway       string
	dataDir       string
	appliedRoutes []string
	mu            sync.Mutex
}

func NewManager(gateway string, dataDir string) *Manager {
	return &Manager{
		gateway: gateway,
		dataDir: dataDir,
	}
}

func (m *Manager) appliedRoutesPath() string {
	return filepath.Join(m.dataDir, "applied_routes.json")
}

func (m *Manager) AddRoutes(cidrs []string) (added int, errors int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, cidr := range cidrs {
		cmdStr := buildAddCommand(cidr, m.gateway)
		args := strings.Fields(cmdStr)
		cmd := exec.Command(args[0], args[1:]...)
		cmd.SysProcAttr = nil
		if err := cmd.Run(); err != nil {
			errors++
			log.Printf("Failed to add route %s: %v", cidr, err)
		} else {
			added++
			m.appliedRoutes = append(m.appliedRoutes, cidr)
		}
	}
	m.saveAppliedRoutes()
	return added, errors
}

func (m *Manager) RemoveAllRoutes() (removed int, errors int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, cidr := range m.appliedRoutes {
		cmdStr := buildDeleteCommand(cidr)
		args := strings.Fields(cmdStr)
		cmd := exec.Command(args[0], args[1:]...)
		if err := cmd.Run(); err != nil {
			errors++
			log.Printf("Failed to delete route %s: %v", cidr, err)
		} else {
			removed++
		}
	}
	m.appliedRoutes = nil
	m.saveAppliedRoutes()
	return removed, errors
}

func (m *Manager) saveAppliedRoutes() {
	data, _ := json.Marshal(m.appliedRoutes)
	_ = os.MkdirAll(m.dataDir, 0755)
	_ = os.WriteFile(m.appliedRoutesPath(), data, 0644)
}

func (m *Manager) CleanupStaleRoutes() {
	data, err := os.ReadFile(m.appliedRoutesPath())
	if err != nil {
		return
	}
	var stale []string
	if err := json.Unmarshal(data, &stale); err != nil {
		return
	}
	if len(stale) == 0 {
		return
	}
	log.Printf("Cleaning up %d stale routes from previous session", len(stale))
	m.appliedRoutes = stale
	m.RemoveAllRoutes()
}

func (m *Manager) HasAppliedRoutes() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.appliedRoutes) > 0
}
