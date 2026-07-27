package ui

import (
	"strings"
	"testing"
)

func TestLoginContactSectionScript(t *testing.T) {
	script := loginContactSectionScript(`C:\Program Files\AnyConnect Split Tunnel\ui-assets\wechat-contact-qr.png`)

	for _, want := range []string{
		"联系作者",
		"微信扫码添加作者",
		"PictureBox",
		"wechat-contact-qr.png",
		"二维码加载失败",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("login contact script missing %q", want)
		}
	}
}

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
