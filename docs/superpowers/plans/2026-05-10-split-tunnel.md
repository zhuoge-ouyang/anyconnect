# AnyConnect Split Tunnel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Windows system tray application in Go that automatically modifies routing tables when Cisco AnyConnect VPN connects, routing Chinese IP ranges through the local gateway for direct access while foreign traffic continues through VPN.

**Architecture:** A single Go binary running as a system tray app with admin privileges. It monitors network adapters for AnyConnect VPN state changes, maintains a Chinese IP database from APNIC, and manages Windows routing table entries. Configuration is persisted in YAML.

**Tech Stack:** Go 1.21+, github.com/getlantern/systray (tray UI), gopkg.in/yaml.v3 (config), golang.org/x/sys/windows (Win32 API)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `cmd/main.go` | Entry point, admin check, orchestration |
| `internal/config/config.go` | YAML config load/save, defaults |
| `internal/ipdb/ipdb.go` | Parse APNIC data, manage china_ip_list.txt |
| `internal/ipdb/ipdb_test.go` | Unit tests for IP parsing logic |
| `internal/route/manager.go` | Add/remove routes via Windows `route` command |
| `internal/route/manager_test.go` | Unit tests for route formatting |
| `internal/monitor/vpn.go` | Poll network adapters, detect AnyConnect |
| `internal/monitor/vpn_test.go` | Unit tests for adapter name matching |
| `internal/tray/tray.go` | System tray icon, menu, notifications |
| `configs/config.yaml` | Default config template |
| `data/china_ip_list.txt` | Bundled fallback Chinese IP list |
| `Makefile` | Build commands |

---

### Task 1: Project Initialization

**Files:**
- Create: `go.mod`
- Create: `cmd/main.go`
- Create: `Makefile`

- [ ] **Step 1: Initialize Go module**

```bash
cd d:\project\anyconnect
go mod init github.com/user/anyconnect-split
```

- [ ] **Step 2: Create minimal main.go that compiles**

Create `cmd/main.go`:

```go
package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func isAdmin() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)
	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}
	return member
}

func main() {
	if !isAdmin() {
		fmt.Println("Error: This program requires administrator privileges.")
		fmt.Println("Please right-click and select 'Run as administrator'.")
		os.Exit(1)
	}
	fmt.Println("AnyConnect Split Tunnel - Running as admin")
}
```

- [ ] **Step 3: Create Makefile**

Create `Makefile`:

```makefile
.PHONY: build run clean test

build:
	go build -ldflags="-H windowsgui" -o bin/anyconnect-split.exe ./cmd/

run:
	go run ./cmd/

clean:
	rm -rf bin/

test:
	go test ./internal/... -v
```

- [ ] **Step 4: Add dependencies and verify build**

```bash
cd d:\project\anyconnect
go get golang.org/x/sys/windows
go build ./cmd/
```

Expected: Compiles with no errors, produces `cmd.exe` or named binary.

- [ ] **Step 5: Commit**

```bash
git init
git add .
git commit -m "feat: initialize project with admin check entry point"
```

---

### Task 2: Config Module

**Files:**
- Create: `internal/config/config.go`
- Create: `configs/config.yaml`

- [ ] **Step 1: Add yaml dependency**

```bash
cd d:\project\anyconnect
go get gopkg.in/yaml.v3
```

- [ ] **Step 2: Implement config module**

Create `internal/config/config.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	OriginalGateway    string    `yaml:"original_gateway"`
	SplitTunnelEnabled bool      `yaml:"split_tunnel_enabled"`
	AutoStart          bool      `yaml:"auto_start"`
	UpdateIntervalDays int       `yaml:"update_interval_days"`
	LastUpdate         time.Time `yaml:"last_update"`
	LogLevel           string    `yaml:"log_level"`
}

func DefaultConfig() *Config {
	return &Config{
		SplitTunnelEnabled: true,
		AutoStart:          false,
		UpdateIntervalDays: 7,
		LogLevel:           "info",
	}
}

func configPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "configs", "config.yaml")
}

func Load() (*Config, error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			_ = cfg.Save()
			return cfg, nil
		}
		return nil, err
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Save() error {
	path := configPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
```

- [ ] **Step 3: Create default config template**

Create `configs/config.yaml`:

```yaml
# AnyConnect Split Tunnel Configuration

# Original gateway (auto-detected, saved here for recovery)
original_gateway: ""

# Enable/disable split tunneling
split_tunnel_enabled: true

# Start with Windows
auto_start: false

# IP database update interval (days)
update_interval_days: 7

# Last IP database update time
last_update: "0001-01-01T00:00:00Z"

# Log level: debug, info, warn, error
log_level: "info"
```

- [ ] **Step 4: Verify compilation**

```bash
cd d:\project\anyconnect
go build ./internal/config/
```

Expected: No errors.

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "feat: add config module with YAML load/save"
```

---

### Task 3: IP Database Module

**Files:**
- Create: `internal/ipdb/ipdb.go`
- Create: `internal/ipdb/ipdb_test.go`
- Create: `data/china_ip_list.txt` (empty placeholder)

- [ ] **Step 1: Write test for APNIC line parsing**

Create `internal/ipdb/ipdb_test.go`:

```go
package ipdb

import (
	"strings"
	"testing"
)

func TestParseAPNICLine(t *testing.T) {
	tests := []struct {
		line     string
		expected string
		valid    bool
	}{
		{"apnic|CN|ipv4|1.0.1.0|256|20110414|allocated", "1.0.1.0/24", true},
		{"apnic|CN|ipv4|1.0.8.0|2048|20110414|allocated", "1.0.8.0/21", true},
		{"apnic|CN|ipv4|1.12.0.0|65536|20110414|allocated", "1.12.0.0/16", true},
		{"apnic|JP|ipv4|1.0.16.0|4096|20110412|allocated", "", false},
		{"apnic|CN|ipv6|2001:250::|35|20110414|allocated", "", false},
		{"# comment line", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		cidr, ok := parseAPNICLine(tt.line)
		if ok != tt.valid {
			t.Errorf("parseAPNICLine(%q): got valid=%v, want %v", tt.line, ok, tt.valid)
		}
		if ok && cidr != tt.expected {
			t.Errorf("parseAPNICLine(%q): got %q, want %q", tt.line, cidr, tt.expected)
		}
	}
}

func TestParseAPNICData(t *testing.T) {
	data := `2|apnic|20230101|12345|19830101|20230101|+1000
apnic|*|asn|*|1234|summary
apnic|CN|ipv4|1.0.1.0|256|20110414|allocated
apnic|CN|ipv4|1.0.8.0|2048|20110414|allocated
apnic|JP|ipv4|1.0.16.0|4096|20110412|allocated
apnic|CN|ipv6|2001:250::|35|20110414|allocated
`
	cidrs := parseAPNICData(strings.NewReader(data))
	if len(cidrs) != 2 {
		t.Fatalf("expected 2 CIDRs, got %d", len(cidrs))
	}
	if cidrs[0] != "1.0.1.0/24" {
		t.Errorf("cidrs[0] = %q, want %q", cidrs[0], "1.0.1.0/24")
	}
	if cidrs[1] != "1.0.8.0/21" {
		t.Errorf("cidrs[1] = %q, want %q", cidrs[1], "1.0.8.0/21")
	}
}

func TestHostCountToCIDR(t *testing.T) {
	tests := []struct {
		count int
		bits  int
	}{
		{256, 24},
		{512, 23},
		{1024, 22},
		{2048, 21},
		{4096, 20},
		{65536, 16},
	}
	for _, tt := range tests {
		got := hostCountToPrefix(tt.count)
		if got != tt.bits {
			t.Errorf("hostCountToPrefix(%d) = %d, want %d", tt.count, got, tt.bits)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd d:\project\anyconnect
go test ./internal/ipdb/ -v
```

Expected: FAIL (functions not defined).

- [ ] **Step 3: Implement IP database module**

Create `internal/ipdb/ipdb.go`:

```go
package ipdb

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const apnicURL = "https://ftp.apnic.net/apnic/stats/apnic/delegated-apnic-latest"

func hostCountToPrefix(count int) int {
	return 32 - int(math.Log2(float64(count)))
}

func parseAPNICLine(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "2|") {
		return "", false
	}
	parts := strings.Split(line, "|")
	if len(parts) < 5 {
		return "", false
	}
	if parts[0] != "apnic" || parts[1] != "CN" || parts[2] != "ipv4" {
		return "", false
	}
	ip := parts[3]
	var count int
	fmt.Sscanf(parts[4], "%d", &count)
	if count <= 0 {
		return "", false
	}
	prefix := hostCountToPrefix(count)
	return fmt.Sprintf("%s/%d", ip, prefix), true
}

func parseAPNICData(r io.Reader) []string {
	var cidrs []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if cidr, ok := parseAPNICLine(scanner.Text()); ok {
			cidrs = append(cidrs, cidr)
		}
	}
	return cidrs
}

type DB struct {
	dataDir string
}

func New(dataDir string) *DB {
	return &DB{dataDir: dataDir}
}

func (db *DB) filePath() string {
	return filepath.Join(db.dataDir, "china_ip_list.txt")
}

func (db *DB) Load() ([]string, error) {
	data, err := os.ReadFile(db.filePath())
	if err != nil {
		return nil, err
	}
	var cidrs []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			cidrs = append(cidrs, line)
		}
	}
	return cidrs, nil
}

func (db *DB) Update() ([]string, error) {
	resp, err := http.Get(apnicURL)
	if err != nil {
		return nil, fmt.Errorf("download APNIC data: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("APNIC returned status %d", resp.StatusCode)
	}
	cidrs := parseAPNICData(resp.Body)
	if len(cidrs) == 0 {
		return nil, fmt.Errorf("no CN IPv4 records found in APNIC data")
	}
	if err := os.MkdirAll(db.dataDir, 0755); err != nil {
		return nil, err
	}
	var sb strings.Builder
	sb.WriteString("# China IP ranges from APNIC\n")
	sb.WriteString("# Auto-generated, do not edit manually\n")
	for _, cidr := range cidrs {
		sb.WriteString(cidr)
		sb.WriteString("\n")
	}
	if err := os.WriteFile(db.filePath(), []byte(sb.String()), 0644); err != nil {
		return nil, err
	}
	return cidrs, nil
}

func (db *DB) NeedsUpdate() bool {
	info, err := os.Stat(db.filePath())
	if err != nil {
		return true
	}
	return info.Size() == 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd d:\project\anyconnect
go test ./internal/ipdb/ -v
```

Expected: All 3 tests PASS.

- [ ] **Step 5: Create empty placeholder data file**

Create `data/china_ip_list.txt`:

```
# China IP ranges from APNIC
# Run the application to auto-download
```

- [ ] **Step 6: Commit**

```bash
git add .
git commit -m "feat: add IP database module with APNIC parser"
```

---

### Task 4: Route Manager Module

**Files:**
- Create: `internal/route/manager.go`
- Create: `internal/route/manager_test.go`

- [ ] **Step 1: Write test for route command formatting**

Create `internal/route/manager_test.go`:

```go
package route

import "testing"

func TestBuildAddCommand(t *testing.T) {
	tests := []struct {
		cidr    string
		gateway string
		want    string
	}{
		{"1.0.1.0/24", "192.168.1.1", "route add 1.0.1.0 mask 255.255.255.0 192.168.1.1 metric 5"},
		{"10.0.0.0/8", "192.168.1.1", "route add 10.0.0.0 mask 255.0.0.0 192.168.1.1 metric 5"},
		{"172.16.0.0/12", "10.0.0.1", "route add 172.16.0.0 mask 255.240.0.0 10.0.0.1 metric 5"},
	}
	for _, tt := range tests {
		got := buildAddCommand(tt.cidr, tt.gateway)
		if got != tt.want {
			t.Errorf("buildAddCommand(%q, %q):\n  got  %q\n  want %q", tt.cidr, tt.gateway, got, tt.want)
		}
	}
}

func TestBuildDeleteCommand(t *testing.T) {
	tests := []struct {
		cidr string
		want string
	}{
		{"1.0.1.0/24", "route delete 1.0.1.0 mask 255.255.255.0"},
		{"10.0.0.0/8", "route delete 10.0.0.0 mask 255.0.0.0"},
	}
	for _, tt := range tests {
		got := buildDeleteCommand(tt.cidr)
		if got != tt.want {
			t.Errorf("buildDeleteCommand(%q): got %q, want %q", tt.cidr, got, tt.want)
		}
	}
}

func TestCIDRToMask(t *testing.T) {
	tests := []struct {
		prefix int
		mask   string
	}{
		{8, "255.0.0.0"},
		{12, "255.240.0.0"},
		{16, "255.255.0.0"},
		{20, "255.255.240.0"},
		{24, "255.255.255.0"},
		{32, "255.255.255.255"},
	}
	for _, tt := range tests {
		got := prefixToMask(tt.prefix)
		if got != tt.mask {
			t.Errorf("prefixToMask(%d) = %q, want %q", tt.prefix, got, tt.mask)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd d:\project\anyconnect
go test ./internal/route/ -v
```

Expected: FAIL (functions not defined).

- [ ] **Step 3: Implement route manager**

Create `internal/route/manager.go`:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd d:\project\anyconnect
go test ./internal/route/ -v
```

Expected: All 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "feat: add route manager with add/remove/cleanup"
```

---

### Task 5: VPN Monitor Module

**Files:**
- Create: `internal/monitor/vpn.go`
- Create: `internal/monitor/vpn_test.go`

- [ ] **Step 1: Write test for adapter name matching**

Create `internal/monitor/vpn_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd d:\project\anyconnect
go test ./internal/monitor/ -v
```

Expected: FAIL.

- [ ] **Step 3: Implement VPN monitor**

Create `internal/monitor/vpn.go`:

```go
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
	StateIdle       VPNState = iota // VPN not connected
	StateConnected                  // VPN connected, routes pending
	StateActive                     // Routes applied
	StateCleaning                   // Cleaning routes
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
	// Use "route print 0.0.0.0" and parse the output
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
			// Skip VPN gateway (typically 0.0.0.0 or the VPN gateway)
			if gateway != "0.0.0.0" && !strings.HasPrefix(gateway, "10.") {
				return gateway, nil
			}
		}
	}
	return "", fmt.Errorf("default gateway not found")
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd d:\project\anyconnect
go test ./internal/monitor/ -v
```

Expected: TestIsCiscoAdapter PASSES.

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "feat: add VPN monitor with adapter detection"
```

---

### Task 6: System Tray UI

**Files:**
- Create: `internal/tray/tray.go`
- Create: `assets/icons/` (icon files)

- [ ] **Step 1: Add systray dependency**

```bash
cd d:\project\anyconnect
go get github.com/getlantern/systray
```

- [ ] **Step 2: Create icon assets**

Create `internal/tray/icons.go` with embedded icon data (simple colored squares as .ico byte arrays):

```go
package tray

import _ "embed"

//go:embed icons/idle.ico
var iconIdle []byte

//go:embed icons/active.ico
var iconActive []byte

//go:embed icons/busy.ico
var iconBusy []byte

//go:embed icons/error.ico
var iconError []byte
```

Create minimal .ico files at `internal/tray/icons/` (16x16 single-color icons). These can be generated programmatically or use embedded byte literals.

Alternative approach - use byte constants directly in `internal/tray/icon_data.go`:

```go
package tray

// Minimal 16x16 ICO format icons (single color)
// These are placeholder icons - replace with proper .ico files for production

var iconIdle = generateIcon(128, 128, 128)   // gray
var iconActive = generateIcon(0, 200, 0)     // green
var iconBusy = generateIcon(255, 200, 0)     // yellow
var iconError = generateIcon(200, 0, 0)      // red

func generateIcon(r, g, b byte) []byte {
	// ICO header (6 bytes) + directory entry (16 bytes) + BMP header + pixels
	// Simplified: 16x16 32-bit BGRA bitmap
	width, height := 16, 16
	bmpSize := width * height * 4
	dataOffset := 6 + 16 + 40 // ICO header + dir entry + BITMAPINFOHEADER

	ico := make([]byte, dataOffset+bmpSize)

	// ICO Header
	ico[0] = 0 // reserved
	ico[1] = 0
	ico[2] = 1 // type: icon
	ico[3] = 0
	ico[4] = 1 // count: 1
	ico[5] = 0

	// Directory entry
	ico[6] = byte(width)  // width
	ico[7] = byte(height) // height
	ico[8] = 0            // color palette
	ico[9] = 0            // reserved
	ico[10] = 1           // color planes
	ico[11] = 0
	ico[12] = 32 // bits per pixel
	ico[13] = 0
	// size of image data
	size := uint32(40 + bmpSize)
	ico[14] = byte(size)
	ico[15] = byte(size >> 8)
	ico[16] = byte(size >> 16)
	ico[17] = byte(size >> 24)
	// offset
	ico[18] = byte(6 + 16)
	ico[19] = 0
	ico[20] = 0
	ico[21] = 0

	// BITMAPINFOHEADER
	off := 22
	ico[off] = 40 // header size
	ico[off+4] = byte(width)
	ico[off+8] = byte(height * 2) // height is doubled in ICO
	ico[off+12] = 1               // planes
	ico[off+14] = 32              // bpp

	// Pixel data (BGRA, bottom-up)
	off = dataOffset
	for i := 0; i < width*height; i++ {
		ico[off+i*4] = b   // blue
		ico[off+i*4+1] = g // green
		ico[off+i*4+2] = r // red
		ico[off+i*4+3] = 255 // alpha
	}

	return ico
}
```

- [ ] **Step 3: Implement system tray**

Create `internal/tray/tray.go`:

```go
package tray

import (
	"fmt"

	"github.com/getlantern/systray"
)

type Actions struct {
	OnToggleSplit func(enabled bool)
	OnUpdateIPDB  func()
	OnViewLog     func()
	OnToggleAuto  func(enabled bool)
	OnQuit        func()
}

type Tray struct {
	actions       Actions
	menuStatus    *systray.MenuItem
	menuToggle    *systray.MenuItem
	menuUpdate    *systray.MenuItem
	menuLog       *systray.MenuItem
	menuAutoStart *systray.MenuItem
	menuQuit      *systray.MenuItem
	splitEnabled  bool
	autoStart     bool
}

func New(actions Actions, splitEnabled bool, autoStart bool) *Tray {
	return &Tray{
		actions:      actions,
		splitEnabled: splitEnabled,
		autoStart:    autoStart,
	}
}

func (t *Tray) Run() {
	systray.Run(t.onReady, t.onExit)
}

func (t *Tray) onReady() {
	systray.SetIcon(iconIdle)
	systray.SetTitle("AnyConnect Split")
	systray.SetTooltip("AnyConnect Split Tunnel - Idle")

	t.menuStatus = systray.AddMenuItem("Status: VPN not connected", "")
	t.menuStatus.Disable()
	systray.AddSeparator()

	t.menuToggle = systray.AddMenuItemCheckbox("Enable Split Tunnel", "Toggle split tunneling", t.splitEnabled)
	t.menuUpdate = systray.AddMenuItem("Update IP Database", "Download latest China IP list")
	t.menuLog = systray.AddMenuItem("View Log", "Open log file")
	systray.AddSeparator()

	t.menuAutoStart = systray.AddMenuItemCheckbox("Start with Windows", "Auto-start on login", t.autoStart)
	t.menuQuit = systray.AddMenuItem("Quit", "Exit application")

	go t.handleClicks()
}

func (t *Tray) onExit() {}

func (t *Tray) handleClicks() {
	for {
		select {
		case <-t.menuToggle.ClickedCh:
			t.splitEnabled = !t.splitEnabled
			if t.splitEnabled {
				t.menuToggle.Check()
			} else {
				t.menuToggle.Uncheck()
			}
			if t.actions.OnToggleSplit != nil {
				t.actions.OnToggleSplit(t.splitEnabled)
			}
		case <-t.menuUpdate.ClickedCh:
			if t.actions.OnUpdateIPDB != nil {
				t.actions.OnUpdateIPDB()
			}
		case <-t.menuLog.ClickedCh:
			if t.actions.OnViewLog != nil {
				t.actions.OnViewLog()
			}
		case <-t.menuAutoStart.ClickedCh:
			t.autoStart = !t.autoStart
			if t.autoStart {
				t.menuAutoStart.Check()
			} else {
				t.menuAutoStart.Uncheck()
			}
			if t.actions.OnToggleAuto != nil {
				t.actions.OnToggleAuto(t.autoStart)
			}
		case <-t.menuQuit.ClickedCh:
			if t.actions.OnQuit != nil {
				t.actions.OnQuit()
			}
			systray.Quit()
			return
		}
	}
}

func (t *Tray) SetStatusIdle() {
	systray.SetIcon(iconIdle)
	systray.SetTooltip("AnyConnect Split Tunnel - Idle")
	t.menuStatus.SetTitle("Status: VPN not connected")
}

func (t *Tray) SetStatusActive(routeCount int) {
	systray.SetIcon(iconActive)
	systray.SetTooltip("AnyConnect Split Tunnel - Active")
	t.menuStatus.SetTitle(fmt.Sprintf("Status: Split active (%d routes)", routeCount))
}

func (t *Tray) SetStatusBusy(msg string) {
	systray.SetIcon(iconBusy)
	systray.SetTooltip("AnyConnect Split Tunnel - Working...")
	t.menuStatus.SetTitle(fmt.Sprintf("Status: %s", msg))
}

func (t *Tray) SetStatusError(msg string) {
	systray.SetIcon(iconError)
	systray.SetTooltip("AnyConnect Split Tunnel - Error")
	t.menuStatus.SetTitle(fmt.Sprintf("Error: %s", msg))
}
```

- [ ] **Step 4: Verify compilation**

```bash
cd d:\project\anyconnect
go build ./internal/tray/
```

Expected: Compiles without errors.

- [ ] **Step 5: Commit**

```bash
git add .
git commit -m "feat: add system tray UI with status icons and menu"
```

---

### Task 7: Main Entry Point - Full Integration

**Files:**
- Modify: `cmd/main.go`

- [ ] **Step 1: Implement full main.go with orchestration**

Replace `cmd/main.go` with:

```go
package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"

	"github.com/user/anyconnect-split/internal/config"
	"github.com/user/anyconnect-split/internal/ipdb"
	"github.com/user/anyconnect-split/internal/monitor"
	"github.com/user/anyconnect-split/internal/route"
	"github.com/user/anyconnect-split/internal/tray"
)

func isAdmin() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)
	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}
	return member
}

func baseDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

func main() {
	if !isAdmin() {
		// Try to relaunch as admin
		exe, _ := os.Executable()
		cmd := exec.Command("cmd", "/C", "start", "", exe)
		cmd.SysProcAttr = &windows.SysProcAttr{
			HideWindow: true,
		}
		// Use ShellExecute runas
		verb := "runas"
		verbPtr, _ := windows.UTF16PtrFromString(verb)
		exePtr, _ := windows.UTF16PtrFromString(exe)
		cwdPtr, _ := windows.UTF16PtrFromString(".")
		windows.ShellExecute(0, verbPtr, exePtr, nil, cwdPtr, windows.SW_NORMAL)
		os.Exit(0)
	}

	// Setup logging
	logFile := filepath.Join(baseDir(), "split-tunnel.log")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	log.Println("AnyConnect Split Tunnel starting...")

	// Load config
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Warning: failed to load config, using defaults: %v", err)
		cfg = config.DefaultConfig()
	}

	// Detect original gateway
	if cfg.OriginalGateway == "" {
		gw, err := monitor.GetDefaultGateway()
		if err != nil {
			log.Printf("Warning: could not detect default gateway: %v", err)
		} else {
			cfg.OriginalGateway = gw
			cfg.Save()
			log.Printf("Detected original gateway: %s", gw)
		}
	}

	// Initialize IP database
	dataDir := filepath.Join(baseDir(), "data")
	db := ipdb.New(dataDir)

	// Check if IP list needs downloading
	if db.NeedsUpdate() {
		log.Println("IP database empty, downloading...")
		if _, err := db.Update(); err != nil {
			log.Printf("Failed to download IP database: %v", err)
		} else {
			cfg.LastUpdate = time.Now()
			cfg.Save()
			log.Println("IP database downloaded successfully")
		}
	}

	// Initialize route manager
	routeMgr := route.NewManager(cfg.OriginalGateway, dataDir)

	// Cleanup stale routes from previous crash
	routeMgr.CleanupStaleRoutes()

	// Initialize VPN monitor
	vpnMon := monitor.New()

	// Define tray actions
	actions := tray.Actions{
		OnToggleSplit: func(enabled bool) {
			cfg.SplitTunnelEnabled = enabled
			cfg.Save()
			if !enabled && routeMgr.HasAppliedRoutes() {
				routeMgr.RemoveAllRoutes()
				log.Println("Split tunnel disabled, routes removed")
			}
		},
		OnUpdateIPDB: func() {
			log.Println("Manual IP database update requested")
			if _, err := db.Update(); err != nil {
				log.Printf("Update failed: %v", err)
			} else {
				cfg.LastUpdate = time.Now()
				cfg.Save()
				log.Println("IP database updated successfully")
			}
		},
		OnViewLog: func() {
			exec.Command("notepad", logFile).Start()
		},
		OnToggleAuto: func(enabled bool) {
			cfg.AutoStart = enabled
			cfg.Save()
			setAutoStart(enabled)
		},
		OnQuit: func() {
			log.Println("Quitting, cleaning up routes...")
			routeMgr.RemoveAllRoutes()
			vpnMon.Stop()
			cfg.Save()
		},
	}

	// Start VPN monitor
	vpnMon.Start()

	// Handle VPN state changes in background
	trayUI := tray.New(actions, cfg.SplitTunnelEnabled, cfg.AutoStart)

	go func() {
		for change := range vpnMon.StateChanges() {
			switch change.NewState {
			case monitor.StateConnected:
				log.Println("VPN connected detected")
				if !cfg.SplitTunnelEnabled {
					vpnMon.SetState(monitor.StateActive)
					continue
				}
				trayUI.SetStatusBusy("Applying routes...")

				// Re-detect gateway if needed
				if cfg.OriginalGateway == "" {
					log.Println("Error: no original gateway configured")
					trayUI.SetStatusError("No gateway configured")
					continue
				}

				cidrs, err := db.Load()
				if err != nil {
					log.Printf("Error loading IP list: %v", err)
					trayUI.SetStatusError("Failed to load IP list")
					continue
				}

				added, errors := routeMgr.AddRoutes(cidrs)
				log.Printf("Routes applied: %d added, %d errors", added, errors)
				trayUI.SetStatusActive(added)
				vpnMon.SetState(monitor.StateActive)

			case monitor.StateCleaning:
				log.Println("VPN disconnected detected, cleaning routes...")
				trayUI.SetStatusBusy("Cleaning routes...")
				removed, _ := routeMgr.RemoveAllRoutes()
				log.Printf("Routes cleaned: %d removed", removed)
				trayUI.SetStatusIdle()
				vpnMon.SetState(monitor.StateIdle)
			}
		}
	}()

	// Auto-update IP database check
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if time.Since(cfg.LastUpdate) > time.Duration(cfg.UpdateIntervalDays)*24*time.Hour {
				log.Println("Auto-updating IP database...")
				if _, err := db.Update(); err != nil {
					log.Printf("Auto-update failed: %v", err)
				} else {
					cfg.LastUpdate = time.Now()
					cfg.Save()
					log.Println("Auto-update completed")
				}
			}
		}
	}()

	// Run tray (blocks until quit)
	trayUI.Run()
}

func setAutoStart(enabled bool) {
	exe, _ := os.Executable()
	key := `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
	var k windows.Handle

	keyPtr, _ := windows.UTF16PtrFromString(key)
	err := windows.RegOpenKeyEx(windows.HKEY_CURRENT_USER, keyPtr, 0, windows.KEY_SET_VALUE, &k)
	if err != nil {
		log.Printf("Failed to open registry: %v", err)
		return
	}
	defer windows.RegCloseKey(k)

	valueName, _ := windows.UTF16PtrFromString("AnyConnectSplit")
	if enabled {
		exeUtf16, _ := windows.UTF16FromString(exe)
		windows.RegSetValueEx(k, valueName, 0, windows.REG_SZ,
			(*byte)(unsafe.Pointer(&exeUtf16[0])),
			uint32(len(exeUtf16)*2))
	} else {
		windows.RegDeleteValue(k, valueName)
	}
}
```

Note: Add `"unsafe"` to imports for the `setAutoStart` function.

- [ ] **Step 2: Update go.mod module path and tidy**

```bash
cd d:\project\anyconnect
go mod tidy
```

- [ ] **Step 3: Verify full build**

```bash
cd d:\project\anyconnect
go build -ldflags="-H windowsgui" -o bin/anyconnect-split.exe ./cmd/
```

Expected: Compiles successfully, produces `bin/anyconnect-split.exe`.

- [ ] **Step 4: Commit**

```bash
git add .
git commit -m "feat: integrate all modules into main entry point"
```

---

### Task 8: Build and Manual Verification

- [ ] **Step 1: Run all unit tests**

```bash
cd d:\project\anyconnect
go test ./internal/... -v
```

Expected: All tests pass.

- [ ] **Step 2: Build release binary**

```bash
cd d:\project\anyconnect
go build -ldflags="-H windowsgui -s -w" -o bin/anyconnect-split.exe ./cmd/
```

The `-s -w` flags strip debug info for smaller binary.

- [ ] **Step 3: Manual test - run as administrator**

1. Right-click `bin/anyconnect-split.exe` → Run as administrator
2. Verify system tray icon appears (gray)
3. Right-click tray icon, verify menu items are present
4. Check `split-tunnel.log` file is created with startup messages

- [ ] **Step 4: Manual test - VPN connection**

1. Connect Cisco AnyConnect VPN
2. Verify tray icon changes to green
3. Open cmd, run `route print | findstr "1.0."` - verify China routes are present
4. Visit https://www.baidu.com - verify fast loading
5. Visit a foreign site - verify still works through VPN

- [ ] **Step 5: Manual test - VPN disconnection**

1. Disconnect AnyConnect VPN
2. Verify tray icon changes back to gray
3. Run `route print | findstr "1.0."` - verify routes are cleaned

- [ ] **Step 6: Final commit**

```bash
git add .
git commit -m "chore: finalize build configuration"
```

---

## Troubleshooting Notes

- If `GetDefaultGateway()` returns wrong gateway: the user may need to manually set `original_gateway` in config.yaml to their router's IP (e.g., 192.168.1.1)
- If routes don't take effect: ensure metric 5 is lower than VPN route metric (usually 1 for AnyConnect, may need adjustment)
- If AnyConnect adapter not detected: check adapter name with `ipconfig /all` and adjust the `isCiscoAdapter` matching logic
