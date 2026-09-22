package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

const hostFileName = "anyconnect-ui.exe"

var nativeHostPath = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), hostFileName)
}

func hostCommand(mode string, request map[string]any) (*exec.Cmd, error) {
	host := nativeHostPath()
	if _, err := os.Stat(host); err != nil {
		return nil, fmt.Errorf("原生界面程序缺失，请使用完整安装包修复：%w", err)
	}
	request["parent_pid"] = os.Getpid()
	if _, ok := request["asset_root"]; !ok {
		request["asset_root"] = filepath.Dir(host)
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(host, mode)
	cmd.Stdin = bytes.NewReader(data)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	return cmd, nil
}

func hostError(err error) {
	log.Printf("Native UI failed: %v", err)
	title, _ := syscall.UTF16PtrFromString("分流守卫：界面启动失败")
	message, _ := syscall.UTF16PtrFromString("原生界面无法启动，请使用完整安装包修复。详细原因见程序日志。")
	dashboardUser32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x10)
}
