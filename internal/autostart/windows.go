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
