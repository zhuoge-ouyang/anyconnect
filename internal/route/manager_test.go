package route

import (
	"errors"
	"net"
	"os"
	"testing"
)

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

func TestBuildAddCommandWithInterface(t *testing.T) {
	got := buildAddCommandWithInterface("1.0.1.0/24", "192.168.1.1", 12)
	want := "route add 1.0.1.0 mask 255.255.255.0 192.168.1.1 metric 5 if 12"
	if got != want {
		t.Errorf("buildAddCommandWithInterface():\n  got  %q\n  want %q", got, want)
	}
}

func TestNetshAddLineIPv4(t *testing.T) {
	got, ok := netshAddLine("1.0.1.0/24", "192.168.1.1", 12)
	want := "interface ipv4 add route prefix=1.0.1.0/24 interface=12 nexthop=192.168.1.1 metric=5 store=active"
	if !ok || got != want {
		t.Fatalf("netshAddLine() = %q, %v; want %q, true", got, ok, want)
	}
}

func TestNetshAddLineIPv6(t *testing.T) {
	got, ok := netshAddLine("2001:db8::/32", "fe80::1", 12)
	want := "interface ipv6 add route prefix=2001:db8::/32 interface=12 nexthop=fe80::1 metric=5 store=active"
	if !ok || got != want {
		t.Fatalf("netshAddLine() = %q, %v; want %q, true", got, ok, want)
	}
}

func TestNetshAddLineSkipsIPv6WithoutInterface(t *testing.T) {
	_, ok := netshAddLine("2001:db8::/32", "fe80::1", 0)
	if ok {
		t.Fatalf("netshAddLine() ok = true, want false")
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

func TestSummarizeCIDRs(t *testing.T) {
	got := SummarizeCIDRs([]string{
		"1.0.0.0/24",
		"1.0.1.0/24",
		"1.0.2.0/24",
		"1.0.3.0/24",
		"2.0.0.0/8",
		"bad-cidr",
		"2.0.0.0/8",
		"2001:db8::/32",
		"2001:db8::/32",
	})
	want := []string{"1.0.0.0/22", "2.0.0.0/8", "2001:db8::/32"}
	if len(got) != len(want) {
		t.Fatalf("SummarizeCIDRs() len = %d, want %d; got %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SummarizeCIDRs()[%d] = %q, want %q; got %#v", i, got[i], want[i], got)
		}
	}
}

func TestIPv4Only(t *testing.T) {
	got := IPv4Only([]string{
		"1.0.1.0/24",
		"2001:db8::/32",
		"bad-cidr",
		"2.0.0.0/8",
	})
	want := []string{"1.0.1.0/24", "2.0.0.0/8"}
	if len(got) != len(want) {
		t.Fatalf("IPv4Only() len = %d, want %d; got %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("IPv4Only()[%d] = %q, want %q; got %#v", i, got[i], want[i], got)
		}
	}
}

func TestExcludeCIDRsContainingIPs(t *testing.T) {
	filtered, excluded := ExcludeCIDRsContainingIPs(
		[]string{"1.0.1.0/24", "122.9.0.0/16", "2001:db8::/32"},
		[]net.IP{net.ParseIP("122.9.125.251"), net.ParseIP("2001:db8::1")},
	)
	wantFiltered := []string{"1.0.1.0/24"}
	wantExcluded := []string{"122.9.0.0/16", "2001:db8::/32"}
	if len(filtered) != len(wantFiltered) {
		t.Fatalf("filtered len = %d, want %d; got %#v", len(filtered), len(wantFiltered), filtered)
	}
	for i := range wantFiltered {
		if filtered[i] != wantFiltered[i] {
			t.Fatalf("filtered[%d] = %q, want %q; got %#v", i, filtered[i], wantFiltered[i], filtered)
		}
	}
	if len(excluded) != len(wantExcluded) {
		t.Fatalf("excluded len = %d, want %d; got %#v", len(excluded), len(wantExcluded), excluded)
	}
	for i := range wantExcluded {
		if excluded[i] != wantExcluded[i] {
			t.Fatalf("excluded[%d] = %q, want %q; got %#v", i, excluded[i], wantExcluded[i], excluded)
		}
	}
}

func TestRouteOutputHasExactCIDR(t *testing.T) {
	output := `
Publish  Type      Met  Prefix                    Idx  Gateway/Interface Name
-------  --------  ---  ------------------------  ---  ------------------------
No       Manual    0    61.80.0.0/13               21  192.168.3.1
No       Manual    0    114.16.0.0/12              21  192.168.3.1
No       Manual    0    211.80.0.0/12              21  192.168.3.1
`
	if !routeOutputHasCIDR(output, "61.80.0.0/13") {
		t.Fatal("routeOutputHasCIDR() did not find exact route 61.80.0.0/13")
	}
	for _, cidr := range []string{"1.80.0.0/12", "14.16.0.0/12"} {
		if routeOutputHasCIDR(output, cidr) {
			t.Fatalf("routeOutputHasCIDR() matched %s by substring", cidr)
		}
	}
}

func TestRoutePrintOutputHasExactIPv4CIDR(t *testing.T) {
	output := `
IPv4 Route Table
===========================================================================
Active Routes:
Network Destination        Netmask          Gateway       Interface  Metric
        61.80.0.0      255.248.0.0      192.168.3.1     192.168.3.57     30
       114.16.0.0      255.240.0.0      192.168.3.1     192.168.3.57     30
       211.80.0.0      255.240.0.0      192.168.3.1     192.168.3.57     30
===========================================================================
`
	if !routePrintOutputHasCIDR(output, "61.80.0.0/13") {
		t.Fatal("routePrintOutputHasCIDR() did not find exact route 61.80.0.0/13")
	}
	for _, cidr := range []string{"1.80.0.0/12", "14.16.0.0/12"} {
		if routePrintOutputHasCIDR(output, cidr) {
			t.Fatalf("routePrintOutputHasCIDR() matched %s by substring", cidr)
		}
	}
}

func TestRouteMissingErrorRecognized(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{errors.New("exit status 1: 找不到元素。"), true},
		{errors.New("exit status 1: Element not found."), true},
		{errors.New("exit status 1: The object was not found."), true},
		{errors.New("exit status 1: 对象已存在。"), false},
	}
	for _, tt := range tests {
		if got := isRouteMissingError(tt.err); got != tt.want {
			t.Fatalf("isRouteMissingError(%q) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestRouteAlreadyExistsErrorRecognized(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{errors.New("exit status 1: 对象已存在。"), true},
		{errors.New("exit status 1: The object already exists."), true},
		{errors.New("exit status 1: Object already exists."), true},
		{errors.New("exit status 1: Element not found."), false},
	}
	for _, tt := range tests {
		if got := isRouteAlreadyExistsError(tt.err); got != tt.want {
			t.Fatalf("isRouteAlreadyExistsError(%q) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestSampleCIDRsKeepsChecksSmall(t *testing.T) {
	cidrs := make([]string, 0, 128)
	for i := 0; i < 128; i++ {
		cidrs = append(cidrs, net.IPv4(10, byte(i), 0, 0).String()+"/16")
	}
	got := sampleCIDRs(cidrs, 24)
	if len(got) != 24 {
		t.Fatalf("sampleCIDRs() len = %d, want 24", len(got))
	}
	seen := make(map[string]struct{}, len(got))
	for _, cidr := range got {
		if _, exists := seen[cidr]; exists {
			t.Fatalf("sampleCIDRs() returned duplicate %q in %#v", cidr, got)
		}
		seen[cidr] = struct{}{}
	}
}

func TestSavedRoutesMatchRejectsPartialStaleRecord(t *testing.T) {
	m := NewManager("192.168.3.1", 21, "", 0, t.TempDir())
	if err := os.MkdirAll(m.dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.appliedRoutesPath(), []byte(`["1.80.0.0/12","14.16.0.0/12"]`), 0644); err != nil {
		t.Fatal(err)
	}

	target := []string{"1.80.0.0/12", "14.16.0.0/12", "61.80.0.0/13"}
	if m.SavedRoutesMatch(target) {
		t.Fatal("SavedRoutesMatch() accepted a partial stale route record")
	}

	sameRoutesDifferentOrder := []string{"14.16.0.0/12", "1.80.0.0/12"}
	if !m.SavedRoutesMatch(sameRoutesDifferentOrder) {
		t.Fatal("SavedRoutesMatch() rejected an equivalent route set")
	}
}

func TestSavedRoutesCoverAllowsExtraHostRoutes(t *testing.T) {
	m := NewManager("192.168.3.1", 21, "", 0, t.TempDir())
	if err := os.MkdirAll(m.dataDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.appliedRoutesPath(), []byte(`["1.80.0.0/12","14.16.0.0/12","220.181.38.148/32"]`), 0644); err != nil {
		t.Fatal(err)
	}

	if !m.SavedRoutesCover([]string{"1.80.0.0/12", "14.16.0.0/12"}) {
		t.Fatal("SavedRoutesCover() rejected saved routes with extra host exceptions")
	}
	if m.SavedRoutesCover([]string{"1.80.0.0/12", "14.16.0.0/12", "61.80.0.0/13"}) {
		t.Fatal("SavedRoutesCover() accepted saved routes missing a base route")
	}
}
