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
