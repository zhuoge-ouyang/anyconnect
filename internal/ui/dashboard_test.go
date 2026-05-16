package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestDashboardScriptParses(t *testing.T) {
	script := dashboardScript(
		`C:\Temp\anyconnect-dashboard-state.json`,
		`C:\Temp\anyconnect-dashboard-commands`,
		`C:\Temp\app.ico`,
	)

	scriptPath := filepath.Join(t.TempDir(), "dashboard.ps1")
	if err := writeUTF16LE(scriptPath, script); err != nil {
		t.Fatalf("write dashboard script: %v", err)
	}

	parserScript := `
$ErrorActionPreference = 'Stop'
$tokens = $null
$parseErrors = $null
[System.Management.Automation.Language.Parser]::ParseFile(__SCRIPT_PATH__, [ref]$tokens, [ref]$parseErrors) | Out-Null
if ($parseErrors.Count -gt 0) {
    $parseErrors | ForEach-Object { Write-Output $_.Message }
    exit 1
}
`
	parserScript = strings.ReplaceAll(parserScript, "__SCRIPT_PATH__", strconv.Quote(scriptPath))
	cmd := exec.Command("powershell", "-NoProfile", "-Command", parserScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("dashboard PowerShell does not parse: %v\n%s", err, string(output))
	}
}

func writeUTF16LE(path, text string) error {
	encoded := utf16.Encode([]rune(text))
	data := make([]byte, 2+len(encoded)*2)
	data[0], data[1] = 0xff, 0xfe
	for i, r := range encoded {
		data[2+i*2] = byte(r)
		data[3+i*2] = byte(r >> 8)
	}
	return os.WriteFile(path, data, 0644)
}
