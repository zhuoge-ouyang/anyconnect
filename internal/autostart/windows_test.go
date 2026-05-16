package autostart

import "testing"

func TestTaskRunCommandQuotesExecutablePath(t *testing.T) {
	got := taskRunCommand(`C:\Program Files\AnyConnect Split\anyconnect-split.exe`)
	want := `"C:\Program Files\AnyConnect Split\anyconnect-split.exe"`
	if got != want {
		t.Fatalf("taskRunCommand() = %q, want %q", got, want)
	}
}
