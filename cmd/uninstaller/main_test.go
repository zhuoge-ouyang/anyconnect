//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, root, name, value string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func fakeHooks() hooks {
	ok := func(string) error { return nil }
	return hooks{ok, ok, ok, func(string) (func() error, error) { return func() error { return nil }, nil }}
}

func TestRemovalModesPreserveUnknownFiles(t *testing.T) {
	for _, preserve := range []bool{true, false} {
		t.Run(map[bool]string{true: "preserve", false: "remove-known-data"}[preserve], func(t *testing.T) {
			root := t.TempDir()
			files := []string{"anyconnect-split.exe", "openconnect/a.dll", "configs/config.yaml", "data/china_ip_list.txt"}
			for _, name := range append(files, "notes.txt", "data/my-file.txt", "split-tunnel.log", "uninstall.exe") {
				put(t, root, name, "test")
			}
			h := fakeHooks()
			removedRecord := false
			scheduled := false
			h.unregister = func(string) error { removedRecord = true; return nil }
			h.scheduleSelf = func(path string) (func() error, error) {
				if path != filepath.Join(root, "uninstall.exe") {
					t.Fatal(path)
				}
				scheduled = true
				return func() error { return nil }, nil
			}
			if err := uninstall(root, files, preserve, h); err != nil {
				t.Fatal(err)
			}
			if !removedRecord || !scheduled {
				t.Fatal("missing finalization")
			}
			for _, name := range []string{"notes.txt", "data/my-file.txt", "uninstall.exe"} {
				if _, err := os.Stat(filepath.Join(root, name)); err != nil {
					t.Fatal(name, err)
				}
			}
			for _, name := range []string{"anyconnect-split.exe", "openconnect/a.dll"} {
				if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
					t.Fatal(name, err)
				}
			}
			for _, name := range []string{"configs/config.yaml", "data/china_ip_list.txt", "split-tunnel.log"} {
				_, err := os.Stat(filepath.Join(root, name))
				if preserve && err != nil || !preserve && !os.IsNotExist(err) {
					t.Fatal(name, err)
				}
			}
		})
	}
}

func TestUnsafePathsRejectedBeforeChanges(t *testing.T) {
	root := t.TempDir()
	app := put(t, root, "anyconnect-split.exe", "test")
	for _, name := range []string{`..\outside.txt`, `C:\outside.txt`, `a\..\outside.txt`, `file:stream`, `*.exe`, `.`, `\\server\share\a`, `NUL`} {
		if _, err := removalPaths(root, []string{"anyconnect-split.exe", name}, false); err == nil {
			t.Errorf("accepted %q", name)
		}
	}
	if _, err := os.Stat(app); err != nil {
		t.Fatal(err)
	}
	if err := validateRoot(`C:\`); err == nil {
		t.Fatal("accepted drive root")
	}
	if err := validateRoot(os.Getenv("USERPROFILE")); err == nil {
		t.Fatal("accepted user profile")
	}
}

func TestRoutesAndProcessesBlockBeforeDeletion(t *testing.T) {
	for _, route := range []string{`["0.0.0.0/1"]`, `broken`} {
		root := t.TempDir()
		app := put(t, root, "anyconnect-split.exe", "test")
		put(t, root, "data/applied_routes.json", route)
		if err := uninstall(root, []string{"anyconnect-split.exe"}, false, fakeHooks()); err == nil {
			t.Fatal("accepted route state")
		}
		if _, err := os.Stat(app); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	app := put(t, root, "anyconnect-split.exe", "test")
	put(t, root, "data/applied_routes.json", `[]`)
	put(t, root, "data/applied_vpn_routes.json", `null`)
	h := fakeHooks()
	h.preflight = func(string) error { return errors.New("running") }
	if err := uninstall(root, []string{"anyconnect-split.exe"}, false, h); err == nil {
		t.Fatal("ignored running process")
	}
	if _, err := os.Stat(app); err != nil {
		t.Fatal(err)
	}
}

func TestFailureRetainsUninstallEntry(t *testing.T) {
	root := t.TempDir()
	put(t, root, "anyconnect-split.exe", "test")
	h := fakeHooks()
	called := false
	h.unregister = func(string) error { called = true; return nil }
	h.scheduleSelf = func(string) (func() error, error) { return nil, errors.New("denied") }
	if err := uninstall(root, []string{"anyconnect-split.exe"}, true, h); err == nil || called {
		t.Fatal("entry removed on failure")
	}
}

func TestUnregisterFailureRestoresExecutable(t *testing.T) {
	root := t.TempDir()
	h := fakeHooks()
	rolledBack := false
	h.scheduleSelf = func(string) (func() error, error) { return func() error { rolledBack = true; return nil }, nil }
	h.unregister = func(string) error { return errors.New("registry denied") }
	if err := uninstall(root, nil, true, h); err == nil || !rolledBack {
		t.Fatal("retry executable not restored")
	}
}

func TestProcessProbeHelper(t *testing.T) {
	if os.Getenv("ANYCONNECT_UNINSTALL_TEST_PROCESS") != "1" {
		return
	}
	fmt.Println("ready")
	// The parent owns this isolated test process and terminates it after inspection.
	time.Sleep(45 * time.Second)
	os.Exit(0)
}

func TestRunningOwnedProcessBlocks(t *testing.T) {
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(root, "test-process.exe")
	if err := os.WriteFile(copyPath, b, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(copyPath, "-test.run=^TestProcessProbeHelper$")
	cmd.Env = append(os.Environ(), "ANYCONNECT_UNINSTALL_TEST_PROCESS=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || strings.TrimSpace(line) != "ready" {
		t.Fatal(line, err)
	}
	if err := runScript(preflightScript(root, os.Getpid())); err == nil {
		t.Fatal("running owned executable not detected")
	}
	var pending string
	rollback, err := scheduleRemoval(copyPath, func(path string) error { pending = path; return nil })
	if err != nil {
		t.Fatal("rename running executable", err)
	}
	if pending == copyPath || !strings.HasSuffix(pending, ".pending-delete.exe") {
		t.Fatal("nonunique reboot target", pending)
	}
	if err := rollback(); err != nil {
		t.Fatal("restore running executable", err)
	}
}

func TestScheduleFailureRestoresFile(t *testing.T) {
	root := t.TempDir()
	path := put(t, root, "uninstall.exe", "test")
	if _, err := scheduleRemoval(path, func(string) error { return errors.New("denied") }); err == nil {
		t.Fatal("ignored failure")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("lost retry executable", err)
	}
}

// Opt-in test uses release files but never actual registry/task/reboot operations.
func TestReleasePayloadRoundTrip(t *testing.T) {
	payload := os.Getenv("ANYCONNECT_UNINSTALL_TEST_PAYLOAD")
	if payload == "" {
		t.Skip("release payload not supplied")
	}
	manifestPath := os.Getenv("ANYCONNECT_UNINSTALL_TEST_MANIFEST")
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	files, err := ownedFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != len(files)+1 {
		t.Fatal("compiled ownership differs from payload")
	}
	for _, preserve := range []bool{true, false} {
		root := t.TempDir()
		for _, f := range manifest.Files {
			b, err := os.ReadFile(filepath.Join(payload, filepath.FromSlash(f.Path)))
			if err != nil {
				t.Fatal(err)
			}
			put(t, root, f.Path, string(b))
		}
		put(t, root, "my-notes.txt", "keep")
		if err := uninstall(root, files, preserve, fakeHooks()); err != nil {
			t.Fatal(err)
		}
		for _, f := range manifest.Files {
			keep := f.Path == "uninstall.exe" || preserve && (strings.HasPrefix(f.Path, "configs/") || strings.HasPrefix(f.Path, "data/"))
			_, err := os.Stat(filepath.Join(root, filepath.FromSlash(f.Path)))
			if keep && err != nil || !keep && !os.IsNotExist(err) {
				t.Fatalf("%s preserve=%v err=%v", f.Path, preserve, err)
			}
		}
		if _, err := os.Stat(filepath.Join(root, "my-notes.txt")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIntegrationCleanupIsTargetScoped(t *testing.T) {
	root := t.TempDir()
	script := `$ErrorActionPreference='Stop'
` + integrationFunction + `
$dir=` + psQuote(root) + `
$target=Join-Path $dir 'anyconnect-split.exe'
$shell=New-Object -ComObject WScript.Shell
$owned=Join-Path $dir '分流守卫.lnk'
$other=Join-Path $dir 'Split Tunnel.lnk'
$s=$shell.CreateShortcut($owned);$s.TargetPath=$target;$s.Save()
$s=$shell.CreateShortcut($other);$s.TargetPath='C:\OtherInstallation\anyconnect-split.exe';$s.Save()
$folder=[pscustomobject]@{Deleted=$false;Target=$target}
$folder | Add-Member ScriptMethod GetTasks { param($flags) return [pscustomobject]@{Name='AnyConnectSplitTunnel';Definition=[pscustomobject]@{Actions=@([pscustomobject]@{Type=0;Path=$this.Target})}} }
$folder | Add-Member ScriptMethod DeleteTask { param($name,$flags) $this.Deleted=$true }
$run=[pscustomobject]@{Deleted=$false;Value='C:\OtherInstallation\anyconnect-split.exe'}
$run | Add-Member ScriptMethod GetValue { param($name,$default) return $this.Value }
$run | Add-Member ScriptMethod DeleteValue { param($name,$throw) $this.Deleted=$true }
Remove-OwnedIntegrations $target $folder $run $shell @($dir)
if (-not $folder.Deleted -or $run.Deleted -or (Test-Path -LiteralPath $owned) -or -not (Test-Path -LiteralPath $other)) { throw 'Ownership cleanup mismatch' }
$folder.Deleted=$false;$folder.Target='C:\OtherInstallation\anyconnect-split.exe';$run.Value='"'+$target+'"'
Remove-OwnedIntegrations $target $folder $run $shell @($dir)
if ($folder.Deleted -or -not $run.Deleted) { throw 'Autostart ownership mismatch' }
`
	if err := runScript(script); err != nil {
		t.Fatal(err)
	}
}

func TestJunctionRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	victim := put(t, outside, "a.dll", "keep")
	link := filepath.Join(root, "openconnect")
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "New-Item -ItemType Junction -Path "+psQuote(link)+" -Target "+psQuote(outside)+" | Out-Null")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create isolated junction: %v %s", err, output)
	}
	if _, err := removalPaths(root, []string{"openconnect/a.dll"}, false); err == nil {
		t.Fatal("accepted junction")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal(err)
	}
}

func TestSystemScriptsAreScopedAndParse(t *testing.T) {
	root := t.TempDir()
	for _, script := range []string{preflightScript(root, os.Getpid()), integrationScript(root)} {
		if strings.Contains(script, "taskkill") || strings.Contains(script, "Stop-Process") || strings.Contains(script, "-Recurse") {
			t.Fatal("broad destructive operation")
		}
		path := put(t, t.TempDir(), "check.ps1", "\ufeff"+script)
		check := `$tokens=$null;$errors=$null;[void][Management.Automation.Language.Parser]::ParseFile(` + psQuote(path) + `,[ref]$tokens,[ref]$errors);if($errors.Count){throw ($errors | Out-String)}`
		if err := runScript(check); err != nil {
			t.Fatal(err)
		}
	}
	// Read-only real process inventory, never touches tasks, shortcuts or routes.
	if err := runScript(preflightScript(root, os.Getpid())); err != nil {
		t.Fatal(err)
	}
}
