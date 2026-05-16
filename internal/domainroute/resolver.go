package domainroute

import (
	"context"
	"log"
	"net"
	"strings"
	"time"
)

const lookupTimeout = 5 * time.Second

func hostRoute(ip net.IP) (string, bool) {
	if v4 := ip.To4(); v4 != nil {
		return v4.String() + "/32", true
	}
	if v6 := ip.To16(); v6 != nil {
		return v6.String() + "/128", true
	}
	return "", false
}

func Resolve(domains []string) []string {
	seen := make(map[string]struct{})
	var routes []string
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" || strings.HasPrefix(domain, "#") {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, domain)
		cancel()
		if err != nil {
			log.Printf("Domain exception lookup failed for %s: %v", domain, err)
			continue
		}
		for _, addr := range addrs {
			cidr, ok := hostRoute(addr.IP)
			if !ok {
				continue
			}
			if _, exists := seen[cidr]; exists {
				continue
			}
			seen[cidr] = struct{}{}
			routes = append(routes, cidr)
		}
	}
	return routes
}
