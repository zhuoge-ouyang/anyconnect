package ui

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/user/anyconnect-split/internal/dashboard"
)

const dashboardWindowTitle = "AnyConnect 分流管理台"

type dashboardLaunchDecision int

const (
	dashboardLaunchStart dashboardLaunchDecision = iota
	dashboardLaunchFocus
	dashboardLaunchWait
)

var (
	dashboardMu                       sync.Mutex
	dashboardCmd                      *exec.Cmd
	dashboardUser32                   = syscall.NewLazyDLL("user32.dll")
	dashboardEnumWindows              = dashboardUser32.NewProc("EnumWindows")
	dashboardGetWindowThreadProcessID = dashboardUser32.NewProc("GetWindowThreadProcessId")
	dashboardGetWindowText            = dashboardUser32.NewProc("GetWindowTextW")
	dashboardIsIconic                 = dashboardUser32.NewProc("IsIconic")
	dashboardShowWindow               = dashboardUser32.NewProc("ShowWindow")
	dashboardSetForegroundWindow      = dashboardUser32.NewProc("SetForegroundWindow")
	dashboardSetWindowPos             = dashboardUser32.NewProc("SetWindowPos")
)

func decideDashboardLaunch(processRunning, windowExists bool) dashboardLaunchDecision {
	if !processRunning {
		return dashboardLaunchStart
	}
	if windowExists {
		return dashboardLaunchFocus
	}
	return dashboardLaunchWait
}

func dashboardWindowForProcess(processID int) uintptr {
	if processID <= 0 {
		return 0
	}
	var found uintptr
	callback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var windowProcessID uint32
		dashboardGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&windowProcessID)))
		if int(windowProcessID) != processID {
			return 1
		}

		text := make([]uint16, 256)
		length, _, _ := dashboardGetWindowText.Call(
			hwnd,
			uintptr(unsafe.Pointer(&text[0])),
			uintptr(len(text)),
		)
		if length == 0 || syscall.UTF16ToString(text) != dashboardWindowTitle {
			return 1
		}
		found = hwnd
		return 0
	})
	dashboardEnumWindows.Call(callback, 0)
	return found
}

func dashboardWindowIsMinimized(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	minimized, _, _ := dashboardIsIconic.Call(hwnd)
	return minimized != 0
}

func focusDashboardWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	const (
		swRestore     = 9
		swpNoSize     = 0x0001
		swpNoMove     = 0x0002
		swpShowWindow = 0x0040
	)
	hwndTopmost := ^uintptr(0)
	hwndNoTopmost := ^uintptr(1)
	flags := uintptr(swpNoSize | swpNoMove | swpShowWindow)

	dashboardShowWindow.Call(hwnd, swRestore)
	dashboardSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, flags)
	dashboardSetWindowPos.Call(hwnd, hwndNoTopmost, 0, 0, 0, 0, flags)
	dashboardSetForegroundWindow.Call(hwnd)
}

func ShowDashboard(store *dashboard.Store, iconPath string) error {
	dashboardMu.Lock()
	defer dashboardMu.Unlock()
	processRunning := dashboardCmd != nil
	var hwnd uintptr
	if processRunning {
		hwnd = dashboardWindowForProcess(dashboardCmd.Process.Pid)
	}
	switch decideDashboardLaunch(processRunning, hwnd != 0) {
	case dashboardLaunchFocus:
		focusDashboardWindow(hwnd)
		return nil
	case dashboardLaunchWait:
		return nil
	}
	cmd, err := hostCommand("dashboard", map[string]any{"snapshot_path": store.SnapshotPath(), "command_dir": store.CommandDir(), "asset_root": filepath.Dir(iconPath)})
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	ready := make(chan error, 1)
	go func() {
		var message struct {
			Ready bool `json:"ready"`
		}
		err := json.NewDecoder(stdout).Decode(&message)
		if err == nil && !message.Ready {
			err = fmt.Errorf("原生管理台未就绪")
		}
		ready <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return fmt.Errorf("原生管理台启动失败：%w", err)
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("原生管理台启动超时")
	}
	dashboardCmd = cmd
	go func() {
		err := cmd.Wait()
		if err != nil { /* Only process status, never login data. */
			log.Printf("Native dashboard exited: %v", err)
		}
		dashboardMu.Lock()
		if dashboardCmd == cmd {
			dashboardCmd = nil
		}
		dashboardMu.Unlock()
	}()
	return nil
}
