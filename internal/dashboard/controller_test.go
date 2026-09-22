package dashboard

import (
	"testing"
)

func TestControllerDispatchesPendingCommands(t *testing.T) {
	store := NewStore(t.TempDir())
	var disconnected, reconnected, viewedLog bool

	controller := NewController(store, Actions{
		OnDisconnect: func() { disconnected = true },
		OnReconnect:  func() { reconnected = true },
		OnViewLog:    func() { viewedLog = true },
	})

	if err := store.WriteCommand(Command{Action: ActionDisconnect}); err != nil {
		t.Fatalf("write disconnect command: %v", err)
	}
	if err := store.WriteCommand(Command{Action: ActionViewLog}); err != nil {
		t.Fatalf("write view log command: %v", err)
	}

	if err := controller.ProcessPendingCommands(); err != nil {
		t.Fatalf("ProcessPendingCommands() error = %v", err)
	}
	if !disconnected || reconnected || !viewedLog {
		t.Fatalf("unexpected simple actions: disconnected=%v reconnected=%v viewedLog=%v", disconnected, reconnected, viewedLog)
	}
}

func TestControllerUpdateSnapshotWritesLatestState(t *testing.T) {
	store := NewStore(t.TempDir())
	controller := NewController(store, Actions{})

	if err := controller.UpdateSnapshot(Snapshot{StatusText: "busy", CurrentSite: "site-a"}); err != nil {
		t.Fatalf("UpdateSnapshot() error = %v", err)
	}

	got, err := controller.ReadSnapshot()
	if err != nil {
		t.Fatalf("ReadSnapshot() error = %v", err)
	}
	if got.StatusText != "busy" || got.CurrentSite != "site-a" {
		t.Fatalf("snapshot not updated: %#v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdateSnapshot() did not stamp UpdatedAt")
	}
}

func TestControllerDispatchesForeignWhitelistCommands(t *testing.T) {
	store := NewStore(t.TempDir())
	var gotDomain, gotCIDR string

	controller := NewController(store, Actions{
		OnAddForeignDomain: func(v string) { gotDomain = v },
		OnAddForeignCIDR:   func(v string) { gotCIDR = v },
	})

	if err := store.WriteCommand(Command{Action: ActionAddForeignDomain, Value: strPtr("openai.com")}); err != nil {
		t.Fatalf("write add_foreign_domain command: %v", err)
	}
	if err := store.WriteCommand(Command{Action: ActionAddForeignCIDR, Value: strPtr("1.2.3.0/24")}); err != nil {
		t.Fatalf("write add_foreign_cidr command: %v", err)
	}
	// Empty value must be rejected by Validate.
	if err := store.WriteCommand(Command{Action: ActionAddForeignDomain, Value: strPtr("")}); err == nil {
		t.Fatal("expected validation error for empty foreign domain value")
	}

	if err := controller.ProcessPendingCommands(); err != nil {
		t.Fatalf("ProcessPendingCommands() error = %v", err)
	}
	if gotDomain != "openai.com" {
		t.Fatalf("foreign domain dispatched = %q, want openai.com", gotDomain)
	}
	if gotCIDR != "1.2.3.0/24" {
		t.Fatalf("foreign cidr dispatched = %q, want 1.2.3.0/24", gotCIDR)
	}
}

func TestControllerDispatchesSplitModeCommand(t *testing.T) {
	store := NewStore(t.TempDir())
	gotMode := ""
	controller := NewController(store, Actions{
		OnSetSplitMode: func(mode string) { gotMode = mode },
	})

	if err := store.WriteCommand(Command{Action: ActionSetSplitMode, Value: strPtr("foreign_direct")}); err != nil {
		t.Fatalf("write set_split_mode command: %v", err)
	}
	if err := controller.ProcessPendingCommands(); err != nil {
		t.Fatalf("ProcessPendingCommands() error = %v", err)
	}
	if gotMode != "foreign_direct" {
		t.Fatalf("split mode dispatched = %q, want foreign_direct", gotMode)
	}
}

func strPtr(s string) *string { return &s }

func TestControllerSelectSite(t *testing.T) {
	store := NewStore(t.TempDir())
	got := ""
	c := NewController(store, Actions{OnSelectSite: func(name string) { got = name }})
	for _, value := range []*string{nil, strPtr(" ")} {
		if err := store.WriteCommand(Command{Action: ActionSelectSite, Value: value}); err == nil {
			t.Fatal("empty site accepted")
		}
	}
	name := "深圳 | 节点;一"
	if err := store.WriteCommand(Command{Action: ActionSelectSite, Value: &name}); err != nil {
		t.Fatal(err)
	}
	if err := c.ProcessPendingCommands(); err != nil {
		t.Fatal(err)
	}
	if got != name {
		t.Fatal(got)
	}
	if err := c.UpdateSnapshot(Snapshot{Sites: []string{name}, ConnectionBusy: true}); err != nil {
		t.Fatal(err)
	}
	s, err := c.ReadSnapshot()
	if err != nil || !s.ConnectionBusy || len(s.Sites) != 1 || s.Sites[0] != name {
		t.Fatalf("%+v %v", s, err)
	}
}
