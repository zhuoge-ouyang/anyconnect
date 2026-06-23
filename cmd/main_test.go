package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/anyconnect-split/internal/config"
	"github.com/user/anyconnect-split/internal/monitor"
	"github.com/user/anyconnect-split/internal/tun"
)

func TestShouldRefreshAfterIPDBUpdateSkipsInactiveTun(t *testing.T) {
	if shouldRefreshAfterIPDBUpdate(true, monitor.StateActive, true, false) {
		t.Fatal("inactive TUN backend should not refresh immediately after IPDB update")
	}
}

func TestShouldRefreshAfterIPDBUpdateAllowsActiveTunAndStaticRoutes(t *testing.T) {
	if !shouldRefreshAfterIPDBUpdate(true, monitor.StateActive, true, true) {
		t.Fatal("active TUN backend should refresh after IPDB update")
	}
	if !shouldRefreshAfterIPDBUpdate(true, monitor.StateConnected, false, false) {
		t.Fatal("static route backend should refresh when monitor state is connected")
	}
	if shouldRefreshAfterIPDBUpdate(false, monitor.StateActive, true, true) {
		t.Fatal("disabled split tunnel should not refresh after IPDB update")
	}
	if shouldRefreshAfterIPDBUpdate(true, monitor.StateIdle, false, false) {
		t.Fatal("idle VPN state should not refresh after IPDB update")
	}
}

func TestShortcutIconUsesCacheBustingFileName(t *testing.T) {
	got := filepath.Base(shortcutIconPath(`C:\App`))
	if got != shortcutIconFileName {
		t.Fatalf("shortcut icon basename = %q, want %q", got, shortcutIconFileName)
	}
	if got == "app.ico" {
		t.Fatal("shortcut icon should not reuse app.ico because Windows caches existing shortcut icons aggressively")
	}
}

func TestConnectionFailureMessageExplainsStaleTunInsteadOfSuggestingNodeChange(t *testing.T) {
	message := connectionFailureMessage(errors.New("sing-box exited before TUN became ready"))
	if strings.Contains(message, "sing-box") {
		t.Fatalf("message exposes internal sing-box error: %q", message)
	}
	if strings.Contains(message, "换个节点") {
		t.Fatalf("stale TUN message should not tell user to change node: %q", message)
	}
	for _, want := range []string{"TUN", "残留", "重新连接"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message = %q, want it to contain %q", message, want)
		}
	}
}

func TestConnectionFailureMessageKeepsNodeAdviceForGenericFailure(t *testing.T) {
	message := connectionFailureMessage(errors.New("authentication failed"))
	if !strings.Contains(message, "换个节点") {
		t.Fatalf("generic connection failure should keep node advice, got %q", message)
	}
}

func TestConnectionFailureMessageExplainsOpenConnectAuthentication(t *testing.T) {
	message := connectionFailureMessage(tun.ErrOpenConnectAuthentication)
	if strings.Contains(message, "换个节点") {
		t.Fatalf("auth failure should not suggest changing nodes, got %q", message)
	}
	for _, want := range []string{"认证失败", "账号", "密码"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message = %q, want it to contain %q", message, want)
		}
	}
}

func TestShouldNotFallbackFromOpenConnectAuthenticationFailure(t *testing.T) {
	if shouldFallbackFromOpenConnect(tun.ErrOpenConnectAuthentication, config.TrafficBackendAuto) {
		t.Fatal("OpenConnect authentication failure should not fall back to Cisco static backend")
	}
	if shouldFallbackFromOpenConnect(errors.New("adapter not found"), config.TrafficBackendOpenTun) {
		t.Fatal("strict OpenConnect backend should not fall back")
	}
	if !shouldFallbackFromOpenConnect(errors.New("adapter not found"), config.TrafficBackendAuto) {
		t.Fatal("auto backend should fall back for non-authentication TUN failures")
	}
}
