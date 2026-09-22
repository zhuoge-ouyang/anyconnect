//go:build windows

package installation

import (
	"fmt"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func TestRegisterIsolatedUserKey(t *testing.T) {
	path := fmt.Sprintf(`Software\AnyConnectUninstallTest-%d-%d`, os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() {
		if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil {
			t.Error(err)
		}
	})
	if err := register(registry.CURRENT_USER, path, `C:\Test App`, "1.0.3.0", ""); err != nil {
		t.Fatal(err)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	for name, want := range map[string]string{"DisplayName": "分流守卫", "DisplayVersion": "1.0.3.0", "UninstallString": `"C:\Test App\uninstall.exe"`, "InstallLocation": `C:\Test App`} {
		got, _, err := k.GetStringValue(name)
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", name, got, err)
		}
	}
	for _, name := range []string{"NoModify", "NoRepair"} {
		got, _, err := k.GetIntegerValue(name)
		if err != nil || got != 1 {
			t.Fatalf("%s: %d %v", name, got, err)
		}
	}
	if _, _, err := k.GetStringValue("Publisher"); err != registry.ErrNotExist {
		t.Fatalf("unexpected publisher: %v", err)
	}
}
