package route

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	routeMetric       = 5
	netshBatchSize    = 200
	netshBaseTimeout  = 30 * time.Second
	netshPerLineLimit = 250 * time.Millisecond
	netshMaxTimeout   = 2 * time.Minute
	routePrintTimeout = 5 * time.Second
	routeCheckSamples = 24
)

type ProgressFunc func(done, total int)

type routeTask struct {
	cidr   string
	line   string
	family int
}

func prefixToMask(prefix int) string {
	mask := net.CIDRMask(prefix, 32)
	return fmt.Sprintf("%d.%d.%d.%d", mask[0], mask[1], mask[2], mask[3])
}

func parseCIDR(cidr string) (network string, prefix int, err error) {
	ip, ipNet, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return "", 0, fmt.Errorf("invalid CIDR: %s", cidr)
	}
	ones, _ := ipNet.Mask.Size()
	return ip.Mask(ipNet.Mask).String(), ones, nil
}

func isIPv4CIDR(cidr string) bool {
	ip, _, err := net.ParseCIDR(strings.TrimSpace(cidr))
	return err == nil && ip.To4() != nil
}

func isIPv6CIDR(cidr string) bool {
	ip, _, err := net.ParseCIDR(strings.TrimSpace(cidr))
	return err == nil && ip.To4() == nil
}

func canonicalCIDR(cidr string) (string, bool) {
	_, ipNet, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return "", false
	}
	return ipNet.String(), true
}

func canonicalCIDRList(cidrs []string) []string {
	seen := make(map[string]struct{}, len(cidrs))
	result := make([]string, 0, len(cidrs))
	for _, cidr := range cidrs {
		key, ok := canonicalCIDR(cidr)
		if !ok {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func sameCIDRSet(a, b []string) bool {
	left := canonicalCIDRList(a)
	right := canonicalCIDRList(b)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func cidrSetCovers(routes, expected []string) bool {
	routeSet := make(map[string]struct{}, len(routes))
	for _, cidr := range canonicalCIDRList(routes) {
		routeSet[cidr] = struct{}{}
	}
	for _, cidr := range canonicalCIDRList(expected) {
		if _, exists := routeSet[cidr]; !exists {
			return false
		}
	}
	return true
}

type cidrBlock struct {
	start  uint32
	prefix int
}

func ipToUint32(ip net.IP) (uint32, bool) {
	v4 := ip.To4()
	if v4 == nil {
		return 0, false
	}
	return uint32(v4[0])<<24 | uint32(v4[1])<<16 | uint32(v4[2])<<8 | uint32(v4[3]), true
}

func uint32ToIP(v uint32) net.IP {
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func cidrToBlock(cidr string) (cidrBlock, bool) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return cidrBlock{}, false
	}
	start, ok := ipToUint32(ip.Mask(ipNet.Mask))
	if !ok {
		return cidrBlock{}, false
	}
	ones, bits := ipNet.Mask.Size()
	if bits != 32 || ones < 0 || ones > 32 {
		return cidrBlock{}, false
	}
	return cidrBlock{start: start, prefix: ones}, true
}

func (b cidrBlock) String() string {
	return fmt.Sprintf("%s/%d", uint32ToIP(b.start).String(), b.prefix)
}

func (b cidrBlock) size() uint32 {
	if b.prefix == 32 {
		return 1
	}
	return 1 << uint(32-b.prefix)
}

func (b cidrBlock) canMerge(other cidrBlock) bool {
	if b.prefix != other.prefix || b.prefix == 0 {
		return false
	}
	size := b.size()
	if b.start+size != other.start {
		return false
	}
	parentSize := size * 2
	return b.start%parentSize == 0
}

// SummarizeCIDRs merges adjacent IPv4 CIDR ranges and deduplicates IPv6 ranges.
func SummarizeCIDRs(cidrs []string) []string {
	ipv4 := summarizeIPv4CIDRs(cidrs)
	ipv6 := dedupeIPv6CIDRs(cidrs)
	return append(ipv4, ipv6...)
}

func IPv4Only(cidrs []string) []string {
	result := make([]string, 0, len(cidrs))
	for _, cidr := range cidrs {
		if isIPv4CIDR(cidr) {
			result = append(result, cidr)
		}
	}
	return result
}

func ExcludeCIDRsContainingIPs(cidrs []string, ips []net.IP) (filtered []string, excluded []string) {
	protected := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			protected = append(protected, v4)
			continue
		}
		if v6 := ip.To16(); v6 != nil {
			protected = append(protected, v6)
		}
	}
	if len(protected) == 0 {
		return append([]string(nil), cidrs...), nil
	}

	filtered = make([]string, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err != nil {
			filtered = append(filtered, cidr)
			continue
		}
		if containsAnyIP(ipNet, protected) {
			excluded = append(excluded, cidr)
			continue
		}
		filtered = append(filtered, cidr)
	}
	return filtered, excluded
}

func containsAnyIP(ipNet *net.IPNet, ips []net.IP) bool {
	networkIsV4 := ipNet.IP.To4() != nil
	for _, ip := range ips {
		ipIsV4 := ip.To4() != nil
		if networkIsV4 != ipIsV4 {
			continue
		}
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

func summarizeIPv4CIDRs(cidrs []string) []string {
	blocks := make([]cidrBlock, 0, len(cidrs))
	seen := make(map[string]struct{}, len(cidrs))
	for _, cidr := range cidrs {
		block, ok := cidrToBlock(strings.TrimSpace(cidr))
		if !ok {
			continue
		}
		key := block.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		blocks = append(blocks, block)
	}
	if len(blocks) == 0 {
		return nil
	}

	for {
		sort.Slice(blocks, func(i, j int) bool {
			if blocks[i].start == blocks[j].start {
				return blocks[i].prefix < blocks[j].prefix
			}
			return blocks[i].start < blocks[j].start
		})

		merged := make([]cidrBlock, 0, len(blocks))
		changed := false
		for i := 0; i < len(blocks); i++ {
			if i+1 < len(blocks) && blocks[i].canMerge(blocks[i+1]) {
				merged = append(merged, cidrBlock{start: blocks[i].start, prefix: blocks[i].prefix - 1})
				i++
				changed = true
				continue
			}
			merged = append(merged, blocks[i])
		}
		blocks = merged
		if !changed {
			break
		}
	}

	result := make([]string, 0, len(blocks))
	for _, block := range blocks {
		result = append(result, block.String())
	}
	return result
}

func dedupeIPv6CIDRs(cidrs []string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, cidr := range cidrs {
		cidr = strings.TrimSpace(cidr)
		if !isIPv6CIDR(cidr) {
			continue
		}
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		key := ipNet.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func buildAddCommand(cidr, gateway string) string {
	network, prefix, _ := parseCIDR(cidr)
	mask := prefixToMask(prefix)
	return fmt.Sprintf("route add %s mask %s %s metric %d", network, mask, gateway, routeMetric)
}

func buildDeleteCommand(cidr string) string {
	network, prefix, _ := parseCIDR(cidr)
	mask := prefixToMask(prefix)
	return fmt.Sprintf("route delete %s mask %s", network, mask)
}

func buildAddCommandWithInterface(cidr, gateway string, interfaceIndex int) string {
	base := buildAddCommand(cidr, gateway)
	if interfaceIndex > 0 {
		return fmt.Sprintf("%s if %d", base, interfaceIndex)
	}
	return base
}

func netshAddLine(cidr, gateway string, interfaceIndex int) (string, bool) {
	if isIPv4CIDR(cidr) {
		if gateway == "" || interfaceIndex <= 0 {
			return "", false
		}
		parts := []string{"interface ipv4 add route", "prefix=" + cidr, fmt.Sprintf("interface=%d", interfaceIndex)}
		parts = append(parts, "nexthop="+gateway, fmt.Sprintf("metric=%d", routeMetric), "store=active")
		return strings.Join(parts, " "), true
	}
	if isIPv6CIDR(cidr) {
		if gateway == "" || interfaceIndex <= 0 {
			return "", false
		}
		return fmt.Sprintf("interface ipv6 add route prefix=%s interface=%d nexthop=%s metric=%d store=active",
			cidr, interfaceIndex, gateway, routeMetric), true
	}
	return "", false
}

func netshDeleteLine(cidr, gateway string, interfaceIndex int) (string, bool) {
	if isIPv4CIDR(cidr) {
		if interfaceIndex <= 0 {
			return "", false
		}
		parts := []string{"interface ipv4 delete route", "prefix=" + cidr, fmt.Sprintf("interface=%d", interfaceIndex)}
		if gateway != "" {
			parts = append(parts, "nexthop="+gateway)
		}
		parts = append(parts, "store=active")
		return strings.Join(parts, " "), true
	}
	if isIPv6CIDR(cidr) {
		if interfaceIndex <= 0 {
			return "", false
		}
		parts := []string{"interface ipv6 delete route", "prefix=" + cidr, fmt.Sprintf("interface=%d", interfaceIndex)}
		if gateway != "" {
			parts = append(parts, "nexthop="+gateway)
		}
		parts = append(parts, "store=active")
		return strings.Join(parts, " "), true
	}
	return "", false
}

type Manager struct {
	gateway              string
	interfaceIndex       int
	ipv6Gateway          string
	ipv6InterfaceIndex   int
	dataDir              string
	appliedRoutes        []string
	appliedRouteFamilies map[string]int
	mu                   sync.Mutex
}

func NewManager(gateway string, interfaceIndex int, ipv6Gateway string, ipv6InterfaceIndex int, dataDir string) *Manager {
	return &Manager{
		gateway:              gateway,
		interfaceIndex:       interfaceIndex,
		ipv6Gateway:          ipv6Gateway,
		ipv6InterfaceIndex:   ipv6InterfaceIndex,
		dataDir:              dataDir,
		appliedRouteFamilies: make(map[string]int),
	}
}

func runHidden(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text != "" {
			return fmt.Errorf("%w: %s", err, text)
		}
		return err
	}
	return nil
}

func netshTimeout(lineCount int) time.Duration {
	timeout := netshBaseTimeout + time.Duration(lineCount)*netshPerLineLimit
	if timeout > netshMaxTimeout {
		return netshMaxTimeout
	}
	return timeout
}

func runNetshScript(ctx context.Context, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	tmp, err := os.CreateTemp("", "anyconnect-routes-*.netsh")
	if err != nil {
		return err
	}
	path := tmp.Name()
	defer os.Remove(path)
	for _, line := range lines {
		if _, err := tmp.WriteString(line + "\r\n"); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, netshTimeout(len(lines)))
	defer cancel()
	return runHidden(ctx, "netsh", "-f", path)
}

func routeFamily(cidr string) int {
	if isIPv4CIDR(cidr) {
		return 4
	}
	if isIPv6CIDR(cidr) {
		return 6
	}
	return 0
}

func (m *Manager) appliedRoutesPath() string {
	return filepath.Join(m.dataDir, "applied_routes.json")
}

func (m *Manager) AddRoutes(cidrs []string) (added int, errors int) {
	return m.AddRoutesContext(context.Background(), cidrs, nil)
}

func (m *Manager) AddRoutesContext(ctx context.Context, cidrs []string, progress ProgressFunc) (added int, errors int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cidrs = SummarizeCIDRs(cidrs)
	if len(cidrs) == 0 {
		m.saveAppliedRoutes()
		return 0, 0
	}

	total := len(cidrs)
	done := 0
	reportProgress(progress, done, total)

	tasks := make([]routeTask, 0, len(cidrs))
	for _, cidr := range cidrs {
		gateway := m.gateway
		family := routeFamily(cidr)
		if family == 6 {
			gateway = m.ipv6Gateway
		}
		if line, ok := netshAddLine(cidr, gateway, m.interfaceIndexFor(cidr)); ok {
			tasks = append(tasks, routeTask{cidr: cidr, line: line, family: family})
		} else {
			errors++
			done++
			log.Printf("Skipped route %s: missing gateway or interface", cidr)
			reportProgress(progress, done, total)
		}
	}

	for start := 0; start < len(tasks); start += netshBatchSize {
		if err := ctx.Err(); err != nil {
			log.Printf("Route add canceled after %d/%d routes: %v", done, total, err)
			m.saveAppliedRoutes()
			return added, errors
		}

		end := start + netshBatchSize
		if end > len(tasks) {
			end = len(tasks)
		}
		chunk := tasks[start:end]
		lines := routeTaskLines(chunk)
		if err := runNetshScript(ctx, lines); err == nil {
			for _, task := range chunk {
				added++
				m.trackRoute(task.cidr, task.family)
			}
			done += len(chunk)
			m.saveAppliedRoutes()
			reportProgress(progress, done, total)
			continue
		} else if ctx.Err() != nil {
			log.Printf("Route add canceled after %d/%d routes: %v", done, total, ctx.Err())
			m.saveAppliedRoutes()
			return added, errors
		} else if isRouteAlreadyExistsError(err) {
			for _, task := range chunk {
				added++
				m.trackRoute(task.cidr, task.family)
			}
			done += len(chunk)
			m.saveAppliedRoutes()
			reportProgress(progress, done, total)
			continue
		} else {
			log.Printf("Route batch add failed (%d-%d/%d), retrying individually: %v", start+1, end, len(tasks), err)
		}

		for _, task := range chunk {
			if err := ctx.Err(); err != nil {
				log.Printf("Route add canceled after %d/%d routes: %v", done, total, err)
				m.saveAppliedRoutes()
				return added, errors
			}
			if err := runNetshScript(ctx, []string{task.line}); err != nil {
				if ctx.Err() != nil {
					log.Printf("Route add canceled after %d/%d routes: %v", done, total, ctx.Err())
					m.saveAppliedRoutes()
					return added, errors
				}
				if isRouteAlreadyExistsError(err) || m.isRoutePresent(task.cidr) {
					added++
					m.trackRoute(task.cidr, task.family)
					done++
					reportProgress(progress, done, total)
					continue
				}
				errors++
				log.Printf("Failed to add route %s: %v", task.cidr, err)
				done++
				reportProgress(progress, done, total)
				continue
			}
			added++
			m.trackRoute(task.cidr, task.family)
			done++
			reportProgress(progress, done, total)
		}
		m.saveAppliedRoutes()
	}
	m.saveAppliedRoutes()
	return added, errors
}

func (m *Manager) RemoveAllRoutes() (removed int, errors int) {
	return m.RemoveAllRoutesContext(context.Background(), nil)
}

func (m *Manager) RemoveAllRoutesContext(ctx context.Context, progress ProgressFunc) (removed int, errors int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	routes := append([]string(nil), m.appliedRoutes...)
	total := len(routes)
	done := 0
	reportProgress(progress, done, total)

	tasks := make([]routeTask, 0, len(routes))
	remaining := make([]routeTask, 0)
	for _, cidr := range routes {
		if line, ok := netshDeleteLine(cidr, m.gatewayFor(cidr), m.interfaceIndexFor(cidr)); ok {
			tasks = append(tasks, routeTask{cidr: cidr, line: line, family: routeFamily(cidr)})
		} else {
			errors++
			remaining = append(remaining, routeTask{cidr: cidr, family: routeFamily(cidr)})
			done++
			reportProgress(progress, done, total)
		}
	}

	for start := 0; start < len(tasks); start += netshBatchSize {
		if err := ctx.Err(); err != nil {
			log.Printf("Route cleanup canceled after %d/%d routes: %v", done, total, err)
			remaining = append(remaining, tasks[start:]...)
			m.replaceAppliedRoutes(remaining)
			return removed, errors
		}

		end := start + netshBatchSize
		if end > len(tasks) {
			end = len(tasks)
		}
		chunk := tasks[start:end]
		lines := routeTaskLines(chunk)
		if err := runNetshScript(ctx, lines); err == nil {
			removed += len(chunk)
			done += len(chunk)
			reportProgress(progress, done, total)
			continue
		} else if ctx.Err() != nil {
			log.Printf("Route cleanup canceled after %d/%d routes: %v", done, total, ctx.Err())
			remaining = append(remaining, tasks[start:]...)
			m.replaceAppliedRoutes(remaining)
			return removed, errors
		} else if isRouteMissingError(err) {
			removed += len(chunk)
			done += len(chunk)
			reportProgress(progress, done, total)
			continue
		} else {
			log.Printf("Route batch delete failed (%d-%d/%d), retrying individually: %v", start+1, end, len(tasks), err)
		}

		for _, task := range chunk {
			if err := ctx.Err(); err != nil {
				log.Printf("Route cleanup canceled after %d/%d routes: %v", done, total, err)
				remaining = append(remaining, task)
				remaining = append(remaining, chunkRoutesAfter(chunk, task.cidr)...)
				remaining = append(remaining, tasks[end:]...)
				m.replaceAppliedRoutes(remaining)
				return removed, errors
			}
			if err := runNetshScript(ctx, []string{task.line}); err != nil {
				if ctx.Err() != nil {
					log.Printf("Route cleanup canceled after %d/%d routes: %v", done, total, ctx.Err())
					remaining = append(remaining, task)
					remaining = append(remaining, chunkRoutesAfter(chunk, task.cidr)...)
					remaining = append(remaining, tasks[end:]...)
					m.replaceAppliedRoutes(remaining)
					return removed, errors
				}
				if isRouteMissingError(err) || !m.isRoutePresent(task.cidr) {
					removed++
					done++
					reportProgress(progress, done, total)
					continue
				}
				errors++
				remaining = append(remaining, task)
				log.Printf("Failed to delete route %s: %v", task.cidr, err)
				done++
				reportProgress(progress, done, total)
				continue
			}
			removed++
			done++
			reportProgress(progress, done, total)
		}
	}
	m.replaceAppliedRoutes(remaining)
	return removed, errors
}

func reportProgress(progress ProgressFunc, done, total int) {
	if progress != nil {
		progress(done, total)
	}
}

func routeTaskLines(tasks []routeTask) []string {
	lines := make([]string, 0, len(tasks))
	for _, task := range tasks {
		lines = append(lines, task.line)
	}
	return lines
}

func routePrintOutput(ctx context.Context, family int, cidr string) (string, bool) {
	network, _, err := parseCIDR(cidr)
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, routePrintTimeout)
	defer cancel()

	args := []string{"print"}
	switch family {
	case 6:
		args = append(args, "-6", network)
	case 4:
		args = append(args, "-4", network)
	default:
		return "", false
	}
	cmd := exec.CommandContext(ctx, "route", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", false
	}
	if err != nil {
		return "", false
	}
	return string(output), true
}

func routePrefixesFromOutput(output string) map[string]struct{} {
	prefixes := make(map[string]struct{})
	for _, field := range strings.Fields(output) {
		field = strings.Trim(field, " \t\r\n,;")
		if strings.HasPrefix(strings.ToLower(field), "prefix=") {
			field = field[len("prefix="):]
		}
		if key, ok := canonicalCIDR(field); ok {
			prefixes[key] = struct{}{}
		}
	}
	return prefixes
}

func routeOutputHasCIDR(output, cidr string) bool {
	key, ok := canonicalCIDR(cidr)
	if !ok {
		return false
	}
	_, exists := routePrefixesFromOutput(output)[key]
	return exists
}

func routePrintOutputHasCIDR(output, cidr string) bool {
	network, prefix, err := parseCIDR(cidr)
	if err != nil {
		return false
	}
	if isIPv4CIDR(cidr) {
		mask := prefixToMask(prefix)
		for _, line := range strings.Split(output, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == network && fields[1] == mask {
				return true
			}
		}
		return false
	}
	return routeOutputHasCIDR(output, cidr)
}

func isRouteMissingError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	markers := []string{
		"找不到元素",
		"element not found",
		"object was not found",
		"cannot find the object",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func isRouteAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	markers := []string{
		"对象已存在",
		"already exists",
		"object already exists",
		"the object already exists",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func sampleCIDRs(cidrs []string, limit int) []string {
	ordered := canonicalCIDRList(cidrs)
	if len(ordered) <= limit {
		return ordered
	}

	ipv4 := make([]string, 0, len(ordered))
	other := make([]string, 0)
	for _, cidr := range ordered {
		if isIPv4CIDR(cidr) {
			ipv4 = append(ipv4, cidr)
			continue
		}
		other = append(other, cidr)
	}
	if len(ipv4) > 0 {
		return evenlySampleCIDRs(ipv4, limit)
	}
	return evenlySampleCIDRs(other, limit)
}

func evenlySampleCIDRs(cidrs []string, limit int) []string {
	if limit <= 0 || len(cidrs) == 0 {
		return nil
	}
	if len(cidrs) <= limit {
		return append([]string(nil), cidrs...)
	}
	if limit == 1 {
		return []string{cidrs[0]}
	}
	samples := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	for i := 0; i < limit; i++ {
		idx := i * (len(cidrs) - 1) / (limit - 1)
		cidr := cidrs[idx]
		if _, exists := seen[cidr]; exists {
			continue
		}
		seen[cidr] = struct{}{}
		samples = append(samples, cidr)
	}
	return samples
}

func chunkRoutesAfter(tasks []routeTask, cidr string) []routeTask {
	for i, task := range tasks {
		if task.cidr == cidr {
			return tasks[i+1:]
		}
	}
	return nil
}

func (m *Manager) replaceAppliedRoutes(tasks []routeTask) {
	m.appliedRoutes = nil
	m.appliedRouteFamilies = make(map[string]int, len(tasks))
	for _, task := range tasks {
		if _, exists := m.appliedRouteFamilies[task.cidr]; exists {
			continue
		}
		m.appliedRoutes = append(m.appliedRoutes, task.cidr)
		m.appliedRouteFamilies[task.cidr] = task.family
	}
	m.saveAppliedRoutes()
}

func (m *Manager) interfaceIndexFor(cidr string) int {
	if routeFamily(cidr) == 6 {
		return m.ipv6InterfaceIndex
	}
	return m.interfaceIndex
}

func (m *Manager) gatewayFor(cidr string) string {
	if routeFamily(cidr) == 6 {
		return m.ipv6Gateway
	}
	return m.gateway
}

func (m *Manager) routeApplicable(cidr string) bool {
	switch routeFamily(cidr) {
	case 4:
		return m.gateway != "" && m.interfaceIndex > 0
	case 6:
		return m.ipv6Gateway != "" && m.ipv6InterfaceIndex > 0
	default:
		return false
	}
}

func (m *Manager) applicableCIDRs(cidrs []string) []string {
	result := make([]string, 0, len(cidrs))
	for _, cidr := range cidrs {
		if m.routeApplicable(cidr) {
			result = append(result, cidr)
		}
	}
	return result
}

func (m *Manager) trackRoute(cidr string, family int) {
	if _, exists := m.appliedRouteFamilies[cidr]; exists {
		return
	}
	m.appliedRoutes = append(m.appliedRoutes, cidr)
	m.appliedRouteFamilies[cidr] = family
}

func (m *Manager) saveAppliedRoutes() {
	data, _ := json.Marshal(m.appliedRoutes)
	_ = os.MkdirAll(m.dataDir, 0755)
	_ = os.WriteFile(m.appliedRoutesPath(), data, 0644)
}

func (m *Manager) loadAppliedRoutes() ([]string, bool) {
	data, err := os.ReadFile(m.appliedRoutesPath())
	if err != nil {
		return nil, false
	}
	var routes []string
	if err := json.Unmarshal(data, &routes); err != nil {
		return nil, false
	}
	return routes, true
}

func (m *Manager) CleanupStaleRoutes() {
	stale, ok := m.loadAppliedRoutes()
	if !ok {
		return
	}
	if len(stale) == 0 {
		return
	}
	log.Printf("Cleaning up %d stale routes from previous session", len(stale))
	m.mu.Lock()
	m.appliedRoutes = stale
	m.appliedRouteFamilies = make(map[string]int, len(stale))
	for _, cidr := range stale {
		m.appliedRouteFamilies[cidr] = routeFamily(cidr)
	}
	m.mu.Unlock()
	m.RemoveAllRoutes()
}

func (m *Manager) HasAppliedRoutes() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.appliedRoutes) > 0
}

func (m *Manager) ForgetAppliedRoutes() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appliedRoutes = nil
	m.appliedRouteFamilies = make(map[string]int)
	m.saveAppliedRoutes()
}

func (m *Manager) HasStaleRoutes() bool {
	stale, ok := m.loadAppliedRoutes()
	if !ok {
		return false
	}
	return len(stale) > 0
}

func (m *Manager) SavedRoutesMatch(expected []string) bool {
	routes, ok := m.loadAppliedRoutes()
	if !ok {
		return false
	}
	return sameCIDRSet(routes, m.applicableCIDRs(expected))
}

func (m *Manager) SavedRoutesCover(expected []string) bool {
	routes, ok := m.loadAppliedRoutes()
	if !ok {
		return false
	}
	return cidrSetCovers(routes, m.applicableCIDRs(expected))
}

func (m *Manager) AreRoutesApplied(expected []string) (bool, int) {
	routes, ok := m.loadAppliedRoutes()
	if !ok {
		return false, 0
	}
	if len(routes) == 0 {
		return false, 0
	}
	if len(expected) > 0 && !cidrSetCovers(routes, m.applicableCIDRs(expected)) {
		return false, len(routes)
	}

	for _, cidr := range sampleCIDRs(routes, routeCheckSamples) {
		family := routeFamily(cidr)
		if family == 0 {
			return false, len(routes)
		}
		if !m.isRoutePresent(cidr) {
			return false, len(routes)
		}
	}

	m.mu.Lock()
	m.appliedRoutes = routes
	m.appliedRouteFamilies = make(map[string]int, len(routes))
	for _, cidr := range routes {
		m.appliedRouteFamilies[cidr] = routeFamily(cidr)
	}
	m.mu.Unlock()
	return true, len(routes)
}

func (m *Manager) isRoutePresent(cidr string) bool {
	output, ok := routePrintOutput(context.Background(), routeFamily(cidr), cidr)
	if !ok {
		return false
	}
	return routePrintOutputHasCIDR(output, cidr)
}

func (m *Manager) GetAppliedRouteCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.appliedRoutes)
}
