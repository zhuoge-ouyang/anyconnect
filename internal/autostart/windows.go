package autostart

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	taskName       = "AnyConnectSplitTunnel"
	legacyRunName  = "AnyConnectSplit"
	runKeyPath     = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
	hideWindowFlag = 0x08000000
)

func exePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

func taskRunCommand(exe string) string {
	return fmt.Sprintf(`"%s"`, exe)
}

func runHidden(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: hideWindowFlag}
	return cmd.Run()
}

func taskSettingsCommand() string {
	return `$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit (New-TimeSpan -Seconds 0); Set-ScheduledTask -TaskName 'AnyConnectSplitTunnel' -Settings $settings | Out-Null`
}

func configureTaskSettings() error {
	if err := runHidden("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", taskSettingsCommand()); err != nil {
		return fmt.Errorf("configure startup task settings: %w", err)
	}
	return nil
}

func removeLegacyRunValue() {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	if err := k.DeleteValue(legacyRunName); err != nil && err != registry.ErrNotExist {
		log.Printf("Failed to remove legacy autostart registry value: %v", err)
	}
}

// Enable creates a per-user logon scheduled task that runs elevated.
// The app edits Windows routes, so HKCU Run is not enough because UAC would
// otherwise block the required administrator permissions.
func Enable() error {
	exe, err := exePath()
	if err != nil {
		return err
	}
	args := []string{
		"/Create",
		"/TN", taskName,
		"/TR", taskRunCommand(exe),
		"/SC", "ONLOGON",
		"/RL", "HIGHEST",
		"/F",
	}
	if err := runHidden("schtasks", args...); err != nil {
		return fmt.Errorf("create startup task: %w", err)
	}
	if err := configureTaskSettings(); err != nil {
		return err
	}
	removeLegacyRunValue()
	return nil
}

func Disable() error {
	removeLegacyRunValue()
	if err := runHidden("schtasks", "/Delete", "/TN", taskName, "/F"); err != nil {
		if !IsEnabled() {
			return nil
		}
		return fmt.Errorf("delete startup task: %w", err)
	}
	return nil
}

func IsEnabled() bool {
	return runHidden("schtasks", "/Query", "/TN", taskName) == nil
}

func reconcileTask(isEnabled func() bool, refresh func() error) (bool, error) {
	if !isEnabled() {
		return false, nil
	}
	return true, refresh()
}

// Reconcile refreshes an existing login task so its action always points to
// the currently running executable. This repairs stale task targets left by an
// executable rename or an application upgrade without enabling autostart for
// users who have disabled it.
func Reconcile() (bool, error) {
	return reconcileTask(IsEnabled, Enable)
}
