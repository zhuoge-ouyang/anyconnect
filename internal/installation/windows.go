//go:build windows

// Package installation contains the shared Windows install/uninstall identity.
package installation

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const (
	StateKey     = `Software\AnyConnectSplitTunnel`
	UninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\AnyConnectSplitTunnel`
	Uninstaller  = "uninstall.exe"
)

// Register writes the standard machine-wide Apps / Programs and Features entry.
// Publisher is deliberately omitted until the developer supplies a real identity.
func Register(root, version, publisher string) error {
	return register(registry.LOCAL_MACHINE, UninstallKey, root, version, publisher)
}

func register(hive registry.Key, path, root, version, publisher string) error {
	k, _, err := registry.CreateKey(hive, path, registry.SET_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return err
	}
	defer k.Close()
	values := map[string]string{
		"DisplayName": "分流守卫", "DisplayVersion": version,
		"InstallLocation": filepath.Clean(root),
		"DisplayIcon":     filepath.Join(root, "anyconnect-split.exe") + ",0",
		"UninstallString": `"` + filepath.Join(root, Uninstaller) + `"`,
		"InstallDate":     time.Now().Format("20060102"),
	}
	if publisher != "" {
		values["Publisher"] = publisher
	}
	for name, value := range values {
		if err := k.SetStringValue(name, value); err != nil {
			return err
		}
	}
	if publisher == "" {
		if err := k.DeleteValue("Publisher"); err != nil && err != registry.ErrNotExist {
			return err
		}
	}
	for _, name := range []string{"NoModify", "NoRepair"} {
		if err := k.SetDWordValue(name, 1); err != nil {
			return err
		}
	}
	return nil
}

func Verify(root string) error {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, UninstallKey, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return fmt.Errorf("未找到此版本的安装记录，请先使用完整安装包覆盖安装：%w", err)
	}
	defer k.Close()
	registered, _, err := k.GetStringValue("InstallLocation")
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(root), filepath.Clean(registered)) {
		return fmt.Errorf("卸载程序位置与 Windows 安装记录不一致，已停止操作")
	}
	return nil
}

func Unregister(root string) error {
	if err := Verify(root); err != nil {
		return err
	}
	// Do not remove another installation's legacy location record.
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, StateKey, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err == nil {
		value, _, readErr := k.GetStringValue("InstallDir")
		k.Close()
		if readErr != nil {
			return readErr
		}
		if strings.EqualFold(filepath.Clean(value), filepath.Clean(root)) {
			if err := deleteMachineKey(StateKey); err != nil {
				return err
			}
		}
	} else if err != registry.ErrNotExist {
		return err
	}
	return deleteMachineKey(UninstallKey)
}

func deleteMachineKey(path string) error {
	parent, err := registry.OpenKey(registry.LOCAL_MACHINE, filepath.Dir(path), registry.WRITE|registry.WOW64_64KEY)
	if err != nil {
		return err
	}
	defer parent.Close()
	return registry.DeleteKey(parent, filepath.Base(path))
}
