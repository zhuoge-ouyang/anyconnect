# Split Mode Switch Controls Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add synchronized split-mode controls to the dashboard and tray menu, persist the selected mode, and apply it to the active routing backend without disconnecting OpenConnect.

**Architecture:** Keep `config.Config.SplitMode` as the persisted source of truth. Dashboard commands and tray clicks call one main-process action; that action validates and saves the mode, updates the tray/dashboard state, updates the TUN session options, and refreshes active rules when appropriate.

**Tech Stack:** Go 1.26.3, Windows WinForms generated through PowerShell, local systray fork, YAML configuration, Go tests.

## Global Constraints

- The labels are `国内直连优先` and `国外 VPN 优先`.
- `domestic_direct` means local direct by default and foreign whitelist through VPN.
- `foreign_direct` means VPN by default and domestic whitelist direct.
- Do not add a third mode, dependency, automatic Git commit, or unrelated refactor.
- Preserve all pre-existing uncommitted workspace changes.

---

### Task 1: Mode validation and transactional persistence

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Create: `cmd/split_mode.go`
- Create: `cmd/split_mode_test.go`

**Interfaces:**
- Produces: `config.NormalizeSplitMode(string) (string, bool)`.
- Produces: `persistSplitMode(*config.Config, string, func() error) (bool, error)`.

- [ ] **Step 1: Write failing mode normalization tests**

```go
func TestNormalizeSplitModeAcceptsSupportedValues(t *testing.T) {
    for input, want := range map[string]string{
        " domestic_direct ": SplitModeDomesticDirect,
        "FOREIGN_DIRECT": SplitModeForeignDirect,
    } {
        got, ok := NormalizeSplitMode(input)
        if !ok || got != want { t.Fatalf("NormalizeSplitMode(%q) = %q, %v", input, got, ok) }
    }
}

func TestNormalizeSplitModeRejectsUnknownValue(t *testing.T) {
    if _, ok := NormalizeSplitMode("unknown"); ok { t.Fatal("unknown mode accepted") }
}
```

- [ ] **Step 2: Run the config tests and verify RED**

Run: `go test ./internal/config -run NormalizeSplitMode -count=1`

Expected: compile failure because `NormalizeSplitMode` is undefined.

- [ ] **Step 3: Implement strict normalization and reuse it from config normalization**

```go
func NormalizeSplitMode(mode string) (string, bool) {
    switch strings.ToLower(strings.TrimSpace(mode)) {
    case SplitModeDomesticDirect:
        return SplitModeDomesticDirect, true
    case SplitModeForeignDirect:
        return SplitModeForeignDirect, true
    default:
        return "", false
    }
}
```

- [ ] **Step 4: Run the config tests and verify GREEN**

Run: `go test ./internal/config -count=1`

Expected: PASS.

- [ ] **Step 5: Write failing persistence tests**

```go
func TestPersistSplitModeSavesChangedMode(t *testing.T) {
    cfg := &config.Config{SplitMode: config.SplitModeDomesticDirect}
    saves := 0
    changed, err := persistSplitMode(cfg, config.SplitModeForeignDirect, func() error { saves++; return nil })
    if err != nil || !changed || saves != 1 || cfg.SplitMode != config.SplitModeForeignDirect {
        t.Fatalf("changed=%v saves=%d mode=%q err=%v", changed, saves, cfg.SplitMode, err)
    }
}

func TestPersistSplitModeRollsBackWhenSaveFails(t *testing.T) {
    cfg := &config.Config{SplitMode: config.SplitModeDomesticDirect}
    changed, err := persistSplitMode(cfg, config.SplitModeForeignDirect, func() error { return errors.New("disk full") })
    if err == nil || changed || cfg.SplitMode != config.SplitModeDomesticDirect {
        t.Fatalf("changed=%v mode=%q err=%v", changed, cfg.SplitMode, err)
    }
}
```

- [ ] **Step 6: Run persistence tests and verify RED**

Run: `go test ./cmd -run PersistSplitMode -count=1`

Expected: compile failure because `persistSplitMode` is undefined.

- [ ] **Step 7: Implement transactional persistence**

```go
func persistSplitMode(cfg *config.Config, target string, save func() error) (bool, error) {
    normalized, ok := config.NormalizeSplitMode(target)
    if !ok { return false, fmt.Errorf("unsupported split mode %q", target) }
    if cfg.SplitMode == normalized { return false, nil }
    previous := cfg.SplitMode
    cfg.SplitMode = normalized
    if err := save(); err != nil {
        cfg.SplitMode = previous
        return false, fmt.Errorf("save split mode: %w", err)
    }
    return true, nil
}
```

- [ ] **Step 8: Run persistence tests and verify GREEN**

Run: `go test ./cmd -run PersistSplitMode -count=1`

Expected: PASS.

### Task 2: Dashboard state and command protocol

**Files:**
- Modify: `internal/dashboard/store.go`
- Modify: `internal/dashboard/controller.go`
- Test: `internal/dashboard/dashboard_test.go`
- Test: `internal/dashboard/controller_test.go`

**Interfaces:**
- Produces: `dashboard.ActionSetSplitMode` with required `Command.Value`.
- Produces: `dashboard.Snapshot.SplitMode string`.
- Produces: `dashboard.Actions.OnSetSplitMode func(string)`.

- [ ] **Step 1: Add failing validation, round-trip, snapshot, and dispatch tests**

```go
func TestCommandValidationAcceptsSupportedSplitModes(t *testing.T) {
    if err := (Command{Action: ActionSetSplitMode, Value: strPtr("domestic_direct")}).Validate(); err != nil {
        t.Fatalf("valid split mode rejected: %v", err)
    }
    if err := (Command{Action: ActionSetSplitMode, Value: strPtr("unknown")}).Validate(); err == nil {
        t.Fatal("unknown split mode accepted")
    }
}
```

Extend the existing snapshot test with `SplitMode: "domestic_direct"` and assert it survives JSON round-trip. Extend the controller test with `OnSetSplitMode` and assert `foreign_direct` is dispatched.

- [ ] **Step 2: Run dashboard tests and verify RED**

Run: `go test ./internal/dashboard -count=1`

Expected: compile failure for the new action, snapshot field, and callback.

- [ ] **Step 3: Implement protocol fields and strict validation**

Add `ActionSetSplitMode`, `Snapshot.SplitMode`, `Actions.OnSetSplitMode`, and controller dispatch. Validate values through `config.NormalizeSplitMode`.

- [ ] **Step 4: Run dashboard tests and verify GREEN**

Run: `go test ./internal/dashboard -count=1`

Expected: PASS.

### Task 3: TUN runtime mode update

**Files:**
- Modify: `internal/tun/session.go`
- Test: `internal/tun/session_test.go`

**Interfaces:**
- Produces: `(*Session).SetSplitMode(string) bool`; valid values update the next `Refresh`/`Start`, invalid values do nothing.

- [ ] **Step 1: Write the failing setter test**

```go
func TestSessionSetSplitModeValidatesAndUpdatesOptions(t *testing.T) {
    s := New(Options{SplitMode: "domestic_direct"})
    if !s.SetSplitMode("foreign_direct") || s.opts.SplitMode != "foreign_direct" {
        t.Fatalf("valid mode was not applied: %q", s.opts.SplitMode)
    }
    if s.SetSplitMode("invalid") || s.opts.SplitMode != "foreign_direct" {
        t.Fatalf("invalid mode changed options: %q", s.opts.SplitMode)
    }
}
```

- [ ] **Step 2: Run the TUN test and verify RED**

Run: `go test ./internal/tun -run SessionSetSplitMode -count=1`

Expected: compile failure because `SetSplitMode` is undefined.

- [ ] **Step 3: Implement the mutex-protected setter**

Normalize through `config.NormalizeSplitMode`, lock `Session.mu`, assign `s.opts.SplitMode`, and return whether the value was valid.

- [ ] **Step 4: Run TUN tests and verify GREEN**

Run: `go test ./internal/tun -count=1`

Expected: PASS.

### Task 4: Tray menu controls and synchronized state

**Files:**
- Modify: `internal/tray/tray.go`
- Test: `internal/tray/click_policy_test.go`

**Interfaces:**
- Extends: `tray.Actions.OnSetSplitMode func(string)`.
- Extends: `tray.Status.SplitMode string`.
- Produces: `(*Tray).SetSplitMode(string)` and `(*Tray).requestSplitMode(string)`.

- [ ] **Step 1: Write failing tray state and no-op click tests**

```go
func TestTraySplitModeStateAndRequest(t *testing.T) {
    calls := 0
    tray := New(Actions{OnSetSplitMode: func(string) { calls++ }}, true, false)
    tray.SetSplitMode(config.SplitModeDomesticDirect)
    tray.requestSplitMode(config.SplitModeDomesticDirect)
    if calls != 0 { t.Fatalf("same mode dispatched %d calls", calls) }
    tray.requestSplitMode(config.SplitModeForeignDirect)
    if calls != 1 { t.Fatalf("new mode dispatched %d calls", calls) }
}
```

Also assert the status listener receives `SplitMode` after `SetSplitMode`.

- [ ] **Step 2: Run tray tests and verify RED**

Run: `go test ./internal/tray -run SplitMode -count=1`

Expected: compile failure for the new callback and methods.

- [ ] **Step 3: Implement submenu and check synchronization**

Create a `分流模式` parent item with checkbox children `国内直连优先` and `国外 VPN 优先`. Use tooltips to explain routing semantics. Add both click channels to `handleClicks`; update checks only through `SetSplitMode` after the main action has persisted the selection.

- [ ] **Step 4: Run tray tests and verify GREEN**

Run: `go test ./internal/tray -count=1`

Expected: PASS.

### Task 5: Dashboard UI and main-process wiring

**Files:**
- Modify: `internal/ui/dashboard.go`
- Test: `internal/ui/dashboard_test.go`
- Modify: `cmd/main.go`

**Interfaces:**
- Consumes: `dashboard.ActionSetSplitMode`, `tray.Actions.OnSetSplitMode`, `tun.Session.SetSplitMode`, and `persistSplitMode`.
- Produces: a visible dashboard ComboBox and one shared runtime action.

- [ ] **Step 1: Write failing dashboard script assertions**

```go
func TestDashboardScriptContainsSplitModeControl(t *testing.T) {
    script := dashboardScript(`C:\Temp\state.json`, `C:\Temp\commands`, `C:\Temp\app.ico`)
    for _, want := range []string{"国内直连优先", "国外 VPN 优先", "set_split_mode", "split_mode"} {
        if !strings.Contains(script, want) { t.Fatalf("dashboard script missing %q", want) }
    }
}
```

Add `"split_mode": "domestic_direct"` to the real WinForms launch fixture.

- [ ] **Step 2: Run UI tests and verify RED**

Run: `go test ./internal/ui -run Dashboard -count=1`

Expected: assertion failure because the script lacks the control and command.

- [ ] **Step 3: Add the dashboard ComboBox and hydration/event logic**

Place the drop-down on the existing checkbox row so the window size does not change. During `Refresh-State`, select the item from `state.split_mode` while `$script:hydrating` is true. On user selection, write `set_split_mode` with `domestic_direct` or `foreign_direct`.

- [ ] **Step 4: Run UI tests and verify GREEN**

Run: `go test ./internal/ui -run Dashboard -count=1`

Expected: PASS, including PowerShell parse and WinForms initialization tests.

- [ ] **Step 5: Wire the shared runtime action in `cmd/main.go`**

Publish `cfg.SplitMode` in each Dashboard snapshot. Initialize the tray with `trayUI.SetSplitMode(cfg.SplitMode)`. Add an `OnSetSplitMode` action that serializes changes with a mutex, calls `persistSplitMode`, updates TUN and tray state, and when VPN is active and split tunneling enabled runs `resetRouteOps()` followed by `applySplitRoutes(true)`.

- [ ] **Step 6: Run focused package tests**

Run: `go test ./internal/config ./internal/dashboard ./internal/tun ./internal/tray ./internal/ui ./cmd -count=1`

Expected: PASS.

- [ ] **Step 7: Format, run the full test suite, build, and inspect diff**

Run:

```powershell
gofmt -w cmd/split_mode.go cmd/split_mode_test.go cmd/main.go internal/config/config.go internal/config/config_test.go internal/dashboard/store.go internal/dashboard/controller.go internal/dashboard/dashboard_test.go internal/dashboard/controller_test.go internal/tun/session.go internal/tun/session_test.go internal/tray/tray.go internal/tray/click_policy_test.go internal/ui/dashboard.go internal/ui/dashboard_test.go
go test ./... -count=1
go build -o artifacts/AnyConnectSplitTunnel.exe ./cmd
git diff --check
git status --short
```

Expected: tests and build exit 0; `git diff --check` reports no whitespace errors; no unrelated paths are added by this task.
