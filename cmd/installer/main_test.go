//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShortcutTargetsUseChineseDisplayName(t *testing.T) {
	targets := shortcutTargets(`C:\Users\Public`, `C:\ProgramData`)
	want := []string{
		filepath.Join(`C:\Users\Public`, "Desktop", shortcutName),
		filepath.Join(`C:\ProgramData`, "Microsoft", "Windows", "Start Menu", "Programs", shortcutName),
	}

	if len(targets) != len(want) {
		t.Fatalf("shortcut target count = %d, want %d", len(targets), len(want))
	}
	for i := range want {
		if targets[i] != want[i] {
			t.Fatalf("target[%d] = %q, want %q", i, targets[i], want[i])
		}
		if filepath.Base(targets[i]) == legacyShortcutName {
			t.Fatalf("target[%d] still uses legacy shortcut name %q", i, legacyShortcutName)
		}
	}
}

func TestInstallerShortcutIconUsesCacheBustingFileName(t *testing.T) {
	got := filepath.Base(shortcutIconPath(`C:\InstallDir`))
	if got != shortcutIconFileName {
		t.Fatalf("shortcut icon basename = %q, want %q", got, shortcutIconFileName)
	}
}

func TestHasBundledTunToolsRequiresOpenConnectAndSingBox(t *testing.T) {
	dir := t.TempDir()
	if hasBundledTunTools(dir) {
		t.Fatal("hasBundledTunTools() returned true without bundled tools")
	}

	openConnect := filepath.Join(dir, "openconnect", "openconnect.exe")
	if err := os.MkdirAll(filepath.Dir(openConnect), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(openConnect, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	if hasBundledTunTools(dir) {
		t.Fatal("hasBundledTunTools() returned true without sing-box.exe")
	}

	singBox := filepath.Join(dir, "tools", "sing-box.exe")
	if err := os.MkdirAll(filepath.Dir(singBox), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(singBox, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	if !hasBundledTunTools(dir) {
		t.Fatal("hasBundledTunTools() returned false with both bundled tools")
	}
}

func TestNativeWizardPreservesInstallSequence(t *testing.T) {
	data, err := os.ReadFile("../../native/ui-host/Installer.cs")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, want := range []string{"--has-bundled-tun-tools", "if(bundled!=0)", "--has-cisco", "--extract-cisco", "--write-install-state", "Process.Start(new ProcessStartInfo(Path.Combine(dir,", "anyconnect-split.exe"} {
		if !strings.Contains(source, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Index(source, "--prepare-overwrite") > strings.Index(source, "--install-payload") {
		t.Fatal("wrong installation ordering")
	}
	if strings.Contains(source, "powershell") {
		t.Fatal("wizard still launches PowerShell")
	}
}

func TestNativeRuntimePresentOnValidationMachine(t *testing.T) {
	if err := requireNativeUIRuntime(); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareOverwriteStopsAppTreeAndOrphanedDashboard(t *testing.T) {
	script := prepareOverwriteScript(`C:\InstallDir\anyconnect-split.exe`)

	for _, want := range []string{
		"taskkill.exe",
		"'/PID'",
		"'/T'",
		"'/F'",
		"$dashboardStatePath = Join-Path $installDir 'data\\dashboard-state.json'",
		"AnyConnect 分流管理台",
		"Get-TargetDashboardProcesses",
		"$stillDashboards.Count -gt 0",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("prepare overwrite script missing %q", want)
		}
	}
}
