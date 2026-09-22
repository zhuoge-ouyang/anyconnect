package main

import (
	"errors"
	"reflect"
	"testing"

	"github.com/user/anyconnect-split/internal/ui"
)

func TestSelectSiteValidatesBeforeDisconnect(t *testing.T) {
	sites := []ui.Site{{Name: "深圳 | 节点;一", Server: "https://sz.example.invalid"}, {Name: "日本", Server: "https://jp.example.invalid"}}
	for _, tt := range []struct {
		name, user, pass   string
		connected, wantErr bool
		wantCalls          int
	}{
		{"未知节点", "dummy", "dummy", true, true, 0},
		{"https://malicious.example", "dummy", "dummy", true, true, 0},
		{"深圳 | 节点;一", "dummy", "dummy", true, false, 0},
		{"日本", "", "", true, true, 0},
		{"日本", "dummy", "dummy", true, false, 2},
		{"深圳 | 节点;一", "dummy", "dummy", false, false, 2},
	} {
		t.Run(tt.name+tt.user, func(t *testing.T) {
			var calls []string
			err := switchSelectedSite(sites, tt.name, sites[0].Name, tt.user, tt.pass, tt.connected,
				func() error { calls = append(calls, "disconnect"); return nil },
				func(site ui.Site, u, p string) error {
					calls = append(calls, "connect")
					if site.Name != tt.name || u != tt.user || p != tt.pass {
						t.Fatal("changed selected site or credentials")
					}
					return nil
				})
			if (err != nil) != tt.wantErr || len(calls) != tt.wantCalls {
				t.Fatalf("err=%v calls=%v", err, calls)
			}
			if len(calls) == 2 && !reflect.DeepEqual(calls, []string{"disconnect", "connect"}) {
				t.Fatal(calls)
			}
		})
	}
}

func TestSelectSiteStopsOnFailureWithoutFallback(t *testing.T) {
	for _, disconnectFails := range []bool{true, false} {
		calls := 0
		err := switchSelectedSite([]ui.Site{{Name: "chosen", Server: "https://example.invalid"}}, "chosen", "old", "dummy", "dummy", true,
			func() error {
				if disconnectFails {
					return errors.New("disconnect")
				}
				return nil
			},
			func(ui.Site, string, string) error { calls++; return errors.New("connect") })
		if err == nil || (disconnectFails && calls != 0) || (!disconnectFails && calls != 1) {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	}
}
