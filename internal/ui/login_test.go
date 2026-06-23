package ui

import "testing"

func TestLoginDefaultSiteAvoidsDomesticPreferred(t *testing.T) {
	sites := []Site{
		{Name: "03.国内专线-深圳节点", Server: "https://api008620.ciscovnp.com:10000"},
		{Name: "23.澳大利亚", Server: "https://api0061.ciscovnp.com:10000"},
		{Name: "24.美国", Server: "https://api0001.ciscovnp.com:10000"},
	}

	got := loginDefaultSite(sites, "03.国内专线-深圳节点")
	if got != "23.澳大利亚" {
		t.Fatalf("loginDefaultSite() = %q, want Australia global site", got)
	}
}

func TestLoginDefaultSiteKeepsGlobalPreferred(t *testing.T) {
	sites := []Site{
		{Name: "03.国内专线-深圳节点", Server: "https://api008620.ciscovnp.com:10000"},
		{Name: "23.澳大利亚", Server: "https://api0061.ciscovnp.com:10000"},
		{Name: "24.美国", Server: "https://api0001.ciscovnp.com:10000"},
	}

	got := loginDefaultSite(sites, "24.美国")
	if got != "24.美国" {
		t.Fatalf("loginDefaultSite() = %q, want explicit global preferred site", got)
	}
}
