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
