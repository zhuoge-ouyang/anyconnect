package tray

import (
	"os/exec"
	"strings"
)

// PromptInput shows a Windows input dialog and returns the entered text.
// The second return value is false if the user cancelled or entered nothing.
func PromptInput(title, label string) (string, bool) {
	script := `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName Microsoft.VisualBasic
$value = [Microsoft.VisualBasic.Interaction]::InputBox('` + escapePS(label) + `', '` + escapePS(title) + `', '')
Write-Output -NoEnumerate $value`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	value := strings.TrimRight(string(out), "\r\n")
	// PowerShell appends a trailing newline; InputBox returns "" on cancel.
	if strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

func escapePS(s string) string {
	s = strings.ReplaceAll(s, "'", "''")
	return s
}
