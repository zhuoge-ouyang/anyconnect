package autostart

import (
	"errors"
	"strings"
	"testing"
)

func TestTaskRunCommandQuotesExecutablePath(t *testing.T) {
	got := taskRunCommand(`C:\Program Files\AnyConnect Split\anyconnect-split.exe`)
	want := `"C:\Program Files\AnyConnect Split\anyconnect-split.exe"`
	if got != want {
		t.Fatalf("taskRunCommand() = %q, want %q", got, want)
	}
}

func TestTaskSettingsCommandKeepsLoginStartupAvailable(t *testing.T) {
	command := taskSettingsCommand()
	for _, required := range []string{
		"-AllowStartIfOnBatteries",
		"-DontStopIfGoingOnBatteries",
		"-StartWhenAvailable",
		"-RestartCount 3",
		"-ExecutionTimeLimit (New-TimeSpan -Seconds 0)",
	} {
		if !strings.Contains(command, required) {
			t.Fatalf("task settings command %q does not contain %q", command, required)
		}
	}
}

func TestReconcileTaskRefreshesEnabledRegistration(t *testing.T) {
	refreshCalls := 0
	enabled, err := reconcileTask(
		func() bool { return true },
		func() error {
			refreshCalls++
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled {
		t.Fatal("reconcileTask() reported an existing task as disabled")
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshCalls)
	}
}

func TestReconcileTaskLeavesDisabledRegistrationAlone(t *testing.T) {
	refreshCalls := 0
	enabled, err := reconcileTask(
		func() bool { return false },
		func() error {
			refreshCalls++
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("reconcileTask() reported a missing task as enabled")
	}
	if refreshCalls != 0 {
		t.Fatalf("refresh calls = %d, want 0", refreshCalls)
	}
}

func TestReconcileTaskReturnsRefreshFailure(t *testing.T) {
	wantErr := errors.New("refresh failed")
	enabled, err := reconcileTask(
		func() bool { return true },
		func() error { return wantErr },
	)
	if !enabled {
		t.Fatal("reconcileTask() lost the enabled state after a refresh failure")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("reconcileTask() error = %v, want %v", err, wantErr)
	}
}
