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
