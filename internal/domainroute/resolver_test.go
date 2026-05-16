package domainroute

import (
	"net"
	"testing"
)

func TestHostRoute(t *testing.T) {
	tests := []struct {
		ip   string
		want string
	}{
		{"203.0.113.8", "203.0.113.8/32"},
		{"2001:db8::8", "2001:db8::8/128"},
	}
	for _, tt := range tests {
		got, ok := hostRoute(net.ParseIP(tt.ip))
		if !ok || got != tt.want {
			t.Fatalf("hostRoute(%q) = %q, %v; want %q, true", tt.ip, got, ok, tt.want)
		}
	}
}
