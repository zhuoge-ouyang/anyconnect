package ui

import (
	"sync/atomic"
	"testing"
)

func TestLoginDefaultSiteKeepsDomesticPreferred(t *testing.T) {
	sites := []Site{
		{Name: "03.国内专线-深圳节点", Server: "https://api008620.ciscovnp.com:10000"},
		{Name: "23.澳大利亚", Server: "https://api0061.ciscovnp.com:10000"},
		{Name: "24.美国", Server: "https://api0001.ciscovnp.com:10000"},
	}

	got := loginDefaultSite(sites, "03.国内专线-深圳节点")
	if got != "03.国内专线-深圳节点" {
		t.Fatalf("loginDefaultSite() = %q, want Shenzhen preferred site", got)
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

func TestLoginDefaultSiteDefaultsToShenzhen(t *testing.T) {
	sites := []Site{
		{Name: "23.澳大利亚", Server: "https://api0061.ciscovnp.com:10000"},
		{Name: "03.国内专线-深圳节点", Server: "https://api008620.ciscovnp.com:10000"},
		{Name: "24.美国", Server: "https://api0001.ciscovnp.com:10000"},
	}

	got := loginDefaultSite(sites, "")
	if got != "03.国内专线-深圳节点" {
		t.Fatalf("loginDefaultSite() = %q, want Shenzhen default site", got)
	}
}

func TestLoginDialogGateRejectsDuplicateUntilFirstReturns(t *testing.T) {
	var gate loginDialogGate
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan LoginResult, 1)
	go func() {
		firstDone <- gate.run(func() LoginResult {
			close(firstStarted)
			<-releaseFirst
			return LoginResult{OK: true}
		})
	}()
	<-firstStarted

	var duplicateCalled atomic.Bool
	duplicate := gate.run(func() LoginResult {
		duplicateCalled.Store(true)
		return LoginResult{OK: true}
	})
	if duplicate.OK {
		t.Fatal("duplicate login dialog was allowed while the first dialog was open")
	}
	if duplicateCalled.Load() {
		t.Fatal("duplicate login dialog callback was invoked")
	}

	close(releaseFirst)
	if first := <-firstDone; !first.OK {
		t.Fatal("first login dialog result was lost")
	}
	if retry := gate.run(func() LoginResult { return LoginResult{OK: true} }); !retry.OK {
		t.Fatal("login dialog gate was not released after the first dialog returned")
	}
}
