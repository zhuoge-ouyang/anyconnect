package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/user/anyconnect-split/internal/dashboard"
	"golang.org/x/sys/windows"
)

var hostBuildOnce sync.Once
var testHost, testHarness, testAssets string
var hostBuildErr error

func TestMain(m *testing.M) {
	code := m.Run()
	if testAssets != "" {
		if err := os.RemoveAll(filepath.Dir(testAssets)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}
	os.Exit(code)
}

func buildTestHost(t *testing.T) (string, string, string) {
	t.Helper()
	hostBuildOnce.Do(func() {
		root, err := filepath.Abs("../..")
		if err != nil {
			hostBuildErr = err
			return
		}
		work, err := os.MkdirTemp("", "anyconnect-native-tests-")
		if err != nil {
			hostBuildErr = err
			return
		}
		testAssets = filepath.Join(work, "assets")
		if err := os.MkdirAll(filepath.Join(testAssets, "ui-assets"), 0755); err != nil {
			hostBuildErr = err
			return
		}
		assets := map[string]string{"app.ico": filepath.Join(root, "internal/tray/app.ico"), "ui-assets/app-brand.png": filepath.Join(root, "cmd/winres/icon.png")}
		for _, name := range []string{"desktop-login-bg.png", "desktop-dashboard-sidebar.png", "wechat-contact-qr.png"} {
			assets["ui-assets/"+name] = filepath.Join(root, "internal/ui/assets", name)
		}
		for dst, src := range assets {
			b, err := os.ReadFile(src)
			if err == nil {
				err = os.WriteFile(filepath.Join(testAssets, filepath.FromSlash(dst)), b, 0644)
			}
			if err != nil {
				hostBuildErr = err
				return
			}
		}
		source := filepath.Join(root, "native/ui-host")
		files, err := filepath.Glob(filepath.Join(source, "*.cs"))
		if err != nil {
			hostBuildErr = err
			return
		}
		compiler := filepath.Join(os.Getenv("WINDIR"), "Microsoft.NET/Framework64/v4.0.30319/csc.exe")
		args := []string{"/nologo", "/platform:x64", "/utf8output", "/r:System.dll", "/r:System.Core.dll", "/r:System.Drawing.dll", "/r:System.Windows.Forms.dll", "/r:System.Windows.Forms.DataVisualization.dll", "/r:System.Web.Extensions.dll", "/win32manifest:" + filepath.Join(source, "app.manifest"), "/resource:" + filepath.Join(source, "Recharge.txt") + ",Recharge.txt"}
		testHost = filepath.Join(testAssets, hostFileName)
		testHarness = filepath.Join(work, "native-tests.exe")
		for _, harness := range []bool{false, true} {
			a := append([]string{}, args...)
			if harness {
				a = append(a, "/target:exe", "/out:"+testHarness, "/main:SplitTunnel.UI.NativeTests", filepath.Join(root, "native/tests/NativeTests.cs"))
			} else {
				a = append(a, "/target:winexe", "/out:"+testHost)
			}
			a = append(a, files...)
			cmd := exec.Command(compiler, a...)
			if output, err := cmd.CombinedOutput(); err != nil {
				hostBuildErr = fmt.Errorf("native host compile: %w\n%s", err, output)
				return
			}
		}
	})
	if hostBuildErr != nil {
		t.Fatal(hostBuildErr)
	}
	return testHost, testHarness, testAssets
}

func TestNativeWinFormsBehavior(t *testing.T) {
	_, harness, assets := buildTestHost(t)
	evidence := t.TempDir()
	if dir := os.Getenv("ANYCONNECT_UI_EVIDENCE"); dir != "" {
		evidence = filepath.Join(dir, time.Now().Format("150405.000000000"))
		t.Logf("Native UI evidence: %s", evidence)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, harness, assets, evidence)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native controls failed: %v\n%s", err, output)
	} else {
		t.Log(string(output))
	}
	entries, err := os.ReadDir(filepath.Join(evidence, "commands"))
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the actual Go protocol validator on every command produced by C#.
	for _, entry := range entries {
		b, err := os.ReadFile(filepath.Join(evidence, "commands", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var command dashboard.Command
		if err := json.Unmarshal(b, &command); err != nil {
			t.Fatal(err)
		}
		if err := command.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDashboardNativeLaunchFocusAndClose(t *testing.T) {
	host, _, assets := buildTestHost(t)
	old := nativeHostPath
	nativeHostPath = func() string { return host }
	defer func() { nativeHostPath = old }()
	store := dashboard.NewStore(t.TempDir())
	if err := store.WriteSnapshot(dashboard.Snapshot{StatusText: "状态：VPN 未连接"}); err != nil {
		t.Fatal(err)
	}
	if err := ShowDashboard(store, filepath.Join(assets, "app.ico")); err != nil {
		t.Fatal(err)
	}
	dashboardMu.Lock()
	cmd := dashboardCmd
	dashboardMu.Unlock()
	if cmd == nil {
		t.Fatal("missing process")
	}
	defer func() { _ = cmd.Process.Kill() }()
	if len(cmd.Args) != 2 || cmd.Args[1] != "dashboard" || !strings.HasSuffix(cmd.Path, hostFileName) {
		t.Fatalf("not native: %v", cmd.Args)
	}
	hwnd := dashboardWindowForProcess(cmd.Process.Pid)
	if hwnd == 0 {
		t.Fatal("native window not found")
	}
	dashboardShowWindow.Call(hwnd, 6)
	if err := ShowDashboard(store, filepath.Join(assets, "app.ico")); err != nil {
		t.Fatal(err)
	}
	dashboardMu.Lock()
	same := dashboardCmd == cmd
	dashboardMu.Unlock()
	if !same || dashboardWindowIsMinimized(hwnd) {
		t.Fatal("focus started duplicate or failed to restore")
	}
	dashboardUser32.NewProc("PostMessageW").Call(hwnd, 0x10, 0, 0)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		dashboardMu.Lock()
		closed := dashboardCmd == nil
		dashboardMu.Unlock()
		if closed {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("native host did not exit after close")
}

func TestHostMissingDoesNotFallback(t *testing.T) {
	old := nativeHostPath
	nativeHostPath = func() string { return filepath.Join(t.TempDir(), "missing.exe") }
	defer func() { nativeHostPath = old }()
	if _, err := hostCommand("login", map[string]any{}); err == nil {
		t.Fatal("missing host accepted")
	}
}

// Optional release smoke check: exercise the actual extracted host, never the installed VPN process.
func TestPackagedNativeHostReady(t *testing.T) {
	host := os.Getenv("ANYCONNECT_PACKAGED_HOST")
	if host == "" {
		t.Skip("set ANYCONNECT_PACKAGED_HOST to verify a release binary")
	}
	old := nativeHostPath
	nativeHostPath = func() string { return host }
	defer func() { nativeHostPath = old }()
	store := dashboard.NewStore(t.TempDir())
	if err := store.WriteSnapshot(dashboard.Snapshot{StatusText: "状态：VPN 未连接", Sites: []string{"隔离测试站点"}}); err != nil {
		t.Fatal(err)
	}
	cmd, err := hostCommand("dashboard", map[string]any{"snapshot_path": store.SnapshotPath(), "command_dir": store.CommandDir()})
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ready := make(chan error, 1)
	go func() {
		var result struct {
			Ready bool `json:"ready"`
		}
		err := json.NewDecoder(stdout).Decode(&result)
		if err == nil && !result.Ready {
			err = fmt.Errorf("host was not ready")
		}
		ready <- err
	}()
	select {
	case err = <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("release host readiness timeout")
	}
	time.Sleep(1200 * time.Millisecond)
	hwnd := dashboardWindowForProcess(cmd.Process.Pid)
	if hwnd == 0 {
		t.Fatal("release dashboard missing")
	}
	dashboardUser32.NewProc("PostMessageW").Call(hwnd, 0x10, 0, 0)
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("release host did not close")
	}
	t.Log("packaged host loaded charts, became ready and accepted close; no VPN action sent")
}

func TestNativeLoginPipeRoundTrip(t *testing.T) {
	host, _, _ := buildTestHost(t)
	old := nativeHostPath
	nativeHostPath = func() string { return host }
	defer func() { nativeHostPath = old }()
	for _, cancelled := range []bool{false, true} {
		type response struct {
			result LoginResult
			err    error
		}
		done := make(chan response, 1)
		go func() {
			r, e := nativeLogin([]Site{{Name: "测试;节点|深圳", Server: "https://example.invalid"}}, "测试;节点|深圳", "测试账号", true)
			done <- response{r, e}
		}()
		var hwnd uintptr
		deadline := time.Now().Add(8 * time.Second)
		for hwnd == 0 && time.Now().Before(deadline) {
			callback := syscall.NewCallback(func(w, _ uintptr) uintptr {
				visible, _, _ := dashboardUser32.NewProc("IsWindowVisible").Call(w)
				if visible == 0 {
					return 1
				}
				var pid uint32
				dashboardGetWindowThreadProcessID.Call(w, uintptr(unsafe.Pointer(&pid)))
				h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
				if err != nil {
					return 1
				}
				defer windows.CloseHandle(h)
				buf := make([]uint16, 2048)
				size := uint32(len(buf))
				if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) != nil || !strings.EqualFold(syscall.UTF16ToString(buf[:size]), host) {
					return 1
				}
				caption := make([]uint16, 256)
				dashboardGetWindowText.Call(w, uintptr(unsafe.Pointer(&caption[0])), 256)
				if syscall.UTF16ToString(caption) == "AnyConnect 分流登录" {
					hwnd = w
					return 0
				}
				return 1
			})
			dashboardEnumWindows.Call(callback, 0)
			if hwnd == 0 {
				time.Sleep(50 * time.Millisecond)
			}
		}
		if hwnd == 0 {
			t.Fatal("native login window did not appear")
		}
		if cancelled {
			dashboardUser32.NewProc("PostMessageW").Call(hwnd, 0x10, 0, 0)
		} else {
			var edits []uintptr
			var connect uintptr
			callback := syscall.NewCallback(func(w, _ uintptr) uintptr {
				class := make([]uint16, 64)
				dashboardUser32.NewProc("GetClassNameW").Call(w, uintptr(unsafe.Pointer(&class[0])), 64)
				if strings.Contains(syscall.UTF16ToString(class), "EDIT") {
					edits = append(edits, w)
				}
				caption := make([]uint16, 128)
				dashboardGetWindowText.Call(w, uintptr(unsafe.Pointer(&caption[0])), 128)
				if syscall.UTF16ToString(caption) == "连接 VPN" {
					connect = w
				}
				return 1
			})
			dashboardUser32.NewProc("EnumChildWindows").Call(hwnd, callback, 0)
			if len(edits) != 2 || connect == 0 {
				dashboardUser32.NewProc("PostMessageW").Call(hwnd, 0x10, 0, 0)
				select {
				case <-done:
				case <-time.After(3 * time.Second):
				}
				t.Fatalf("login controls not found: edits=%d connect=%d", len(edits), connect)
			}
			password, _ := syscall.UTF16PtrFromString("dummy-only-测试-密码")
			dashboardUser32.NewProc("SendMessageW").Call(edits[1], 0x000c, 0, uintptr(unsafe.Pointer(password)))
			dashboardUser32.NewProc("PostMessageW").Call(connect, 0x00f5, 0, 0)
		}
		select {
		case r := <-done:
			if r.err != nil {
				t.Fatal("login pipe failed", r.err)
			}
			if cancelled {
				if r.result.OK {
					t.Fatal("cancel accepted")
				}
			} else if !r.result.OK || r.result.SiteName != "测试;节点|深圳" || r.result.Username != "测试账号" || r.result.Password != "dummy-only-测试-密码" || !r.result.Remember {
				t.Fatal("login pipe result mismatch")
			}
		case <-time.After(5 * time.Second):
			dashboardUser32.NewProc("PostMessageW").Call(hwnd, 0x10, 0, 0)
			t.Fatal("login pipe did not return")
		}
	}
}

func TestDashboardLaunchDecision(t *testing.T) {
	for _, v := range []struct {
		running, window bool
		want            dashboardLaunchDecision
	}{{false, false, dashboardLaunchStart}, {true, false, dashboardLaunchWait}, {true, true, dashboardLaunchFocus}} {
		if got := decideDashboardLaunch(v.running, v.window); got != v.want {
			t.Fatal(got, v.want)
		}
	}
}

func TestNativeUIParentHelper(t *testing.T) {
	if os.Getenv("ANYCONNECT_UI_PARENT_HELPER") != "1" {
		return
	}
	nativeHostPath = func() string { return os.Getenv("ANYCONNECT_UI_HOST") }
	dir := os.Getenv("ANYCONNECT_UI_STORE")
	store := dashboard.NewStore(dir)
	if err := store.WriteSnapshot(dashboard.Snapshot{StatusText: "状态：VPN 未连接"}); err != nil {
		os.Exit(2)
	}
	if err := ShowDashboard(store, filepath.Join(filepath.Dir(nativeHostPath()), "app.ico")); err != nil {
		os.Exit(3)
	}
	dashboardMu.Lock()
	pid := dashboardCmd.Process.Pid
	dashboardMu.Unlock()
	fmt.Println(pid)
	os.Exit(0)
}

func TestNativeHostExitsWithParent(t *testing.T) {
	host, _, _ := buildTestHost(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestNativeUIParentHelper$")
	cmd.Env = append(os.Environ(), "ANYCONNECT_UI_PARENT_HELPER=1", "ANYCONNECT_UI_HOST="+host, "ANYCONNECT_UI_STORE="+t.TempDir())
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(out.String()))
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err == windows.ERROR_INVALID_PARAMETER {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	result, err := windows.WaitForSingleObject(h, 5000)
	if err != nil || result != windows.WAIT_OBJECT_0 {
		t.Fatalf("orphaned native host: %d %v", result, err)
	}
}

func TestProductionUIHasNoScriptHost(t *testing.T) {
	for _, name := range []string{"login.go", "dashboard.go", "host.go"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"exec.Command(\"powershell", "ExecutionPolicy", "CreateTemp("} {
			if bytes.Contains(b, []byte(bad)) {
				t.Fatalf("%s still uses %s", name, bad)
			}
		}
	}
}
