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

func TestWizardSkipsCiscoWhenBundledTunToolsArePresent(t *testing.T) {
	if !strings.Contains(wizardScript, "--has-bundled-tun-tools") {
		t.Fatal("wizardScript does not check bundled TUN tools before requiring Cisco")
	}
	if !strings.Contains(wizardScript, "(-not $hasBundledTun) -and (-not $hasCisco)") {
		t.Fatal("wizardScript should only install Cisco when bundled TUN tools and Cisco are both unavailable")
	}
}

func TestWizardStartsInstalledAppWithoutWaitingForLongRunningProcess(t *testing.T) {
	if strings.Contains(wizardScript, "Invoke-InstallerCommand -Arguments @('--start-app', $installDir)") {
		t.Fatal("wizardScript waits on --start-app, which can block until the installed app exits")
	}
	if !strings.Contains(wizardScript, "Start-InstalledApp $installDir") {
		t.Fatal("wizardScript should start the installed app without waiting on the app process tree")
	}
}

func TestWizardPreparesOverwriteBeforeCopyingPayload(t *testing.T) {
	prepareIndex := strings.Index(wizardScript, "('--prepare-overwrite', $installDir)")
	if prepareIndex < 0 {
		t.Fatal("wizardScript should prepare overwrite installs before copying payload")
	}
	installIndex := strings.Index(wizardScript, "('--install-payload', $installDir)")
	if installIndex < 0 {
		t.Fatal("wizardScript should install payload")
	}
	if prepareIndex > installIndex {
		t.Fatal("wizardScript should stop the existing app before overwriting files")
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

func TestWizardRecordsInstallDirectoryForFutureOverwrite(t *testing.T) {
	if !strings.Contains(wizardScript, "('--write-install-state', $installDir)") {
		t.Fatal("wizardScript should persist the install directory for future overwrite installs")
	}
	if installRegistryPath != `Software\AnyConnectSplitTunnel` {
		t.Fatalf("installRegistryPath = %q, want stable product registry path", installRegistryPath)
	}
}
