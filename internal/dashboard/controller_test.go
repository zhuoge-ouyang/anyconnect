package dashboard

import (
	"testing"
)

func TestControllerDispatchesPendingCommands(t *testing.T) {
	store := NewStore(t.TempDir())
	var disconnected, reconnected, viewedLog bool
	var splitValue *bool

	controller := NewController(store, Actions{
		OnDisconnect: func() { disconnected = true },
		OnReconnect:  func() { reconnected = true },
		OnToggleSplit: func(enabled bool) {
			splitValue = &enabled
		},
		OnViewLog: func() { viewedLog = true },
	})

	if err := store.WriteCommand(Command{Action: ActionDisconnect}); err != nil {
		t.Fatalf("write disconnect command: %v", err)
	}
	if err := store.WriteCommand(Command{Action: ActionToggleSplit, Enabled: boolPtr(true)}); err != nil {
		t.Fatalf("write toggle command: %v", err)
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
	if splitValue == nil || !*splitValue {
		t.Fatalf("toggle split was not dispatched with true: %#v", splitValue)
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
