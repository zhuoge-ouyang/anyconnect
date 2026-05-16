package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotStoreWritesReadableSnapshot(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	updatedAt := time.Date(2026, 5, 16, 11, 30, 0, 0, time.UTC)

	want := Snapshot{
		StatusText:          "TUN split active",
		CurrentSite:         "03.domestic-shenzhen",
		SplitTunnelEnabled:  true,
		AutoStartEnabled:    true,
		Backend:             "openconnect_tun",
		RouteCount:          128,
		LastIPDBUpdate:      updatedAt,
		OriginalGateway:     "192.168.1.1",
		OriginalInterface:   12,
		OriginalIPv6Gateway: "fe80::1",
		OriginalIPv6IfIndex: 18,
		IPv6SplitEnabled:    false,
		LastError:           "",
		UpdatedAt:           updatedAt,
	}

	if err := store.WriteSnapshot(want); err != nil {
		t.Fatalf("WriteSnapshot() error = %v", err)
	}

	data, err := os.ReadFile(store.SnapshotPath())
	if err != nil {
		t.Fatalf("snapshot file was not written: %v", err)
	}

	var got Snapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("snapshot json is invalid: %v", err)
	}
	if got.StatusText != want.StatusText || got.CurrentSite != want.CurrentSite {
		t.Fatalf("snapshot mismatch: got status/site %q/%q", got.StatusText, got.CurrentSite)
	}
	if got.RouteCount != want.RouteCount || got.OriginalInterface != want.OriginalInterface {
		t.Fatalf("diagnostic fields were not preserved: got routes=%d interface=%d", got.RouteCount, got.OriginalInterface)
	}
}

func TestCommandRoundTripConsumesOnlyValidCommands(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	valid := Command{Action: ActionToggleSplit, Enabled: boolPtr(false)}
	if err := store.WriteCommand(valid); err != nil {
		t.Fatalf("WriteCommand(valid) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(store.CommandDir(), "bad.json"), []byte(`{"action":"unknown"}`), 0644); err != nil {
		t.Fatalf("write bad command: %v", err)
	}

	commands, err := store.ConsumeCommands()
	if err != nil {
		t.Fatalf("ConsumeCommands() error = %v", err)
	}
	if len(commands) != 1 {
		t.Fatalf("ConsumeCommands() got %d valid commands, want 1", len(commands))
	}
	if commands[0].Action != ActionToggleSplit {
		t.Fatalf("command action = %q, want %q", commands[0].Action, ActionToggleSplit)
	}
	if commands[0].Enabled == nil || *commands[0].Enabled {
		t.Fatalf("command enabled value was not preserved: %#v", commands[0].Enabled)
	}

	remaining, err := os.ReadDir(store.CommandDir())
	if err != nil {
		t.Fatalf("read command dir: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("commands were not consumed; remaining=%d", len(remaining))
	}
}

func TestCommandValidationRejectsMissingToggleValues(t *testing.T) {
	if err := (Command{Action: ActionToggleAutoStart}).Validate(); err == nil {
		t.Fatal("Validate() accepted toggle autostart command without enabled value")
	}
	if err := (Command{Action: ActionViewLog}).Validate(); err != nil {
		t.Fatalf("Validate() rejected simple command: %v", err)
	}
}

func boolPtr(v bool) *bool {
	return &v
}
