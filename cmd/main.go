package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func isAdmin() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)
	token := windows.Token(0)
	member, err := token.IsMember(sid)
	if err != nil {
		return false
	}
	return member
}

func main() {
	if !isAdmin() {
		fmt.Println("Error: This program requires administrator privileges.")
		fmt.Println("Please right-click and select 'Run as administrator'.")
		os.Exit(1)
	}
	fmt.Println("AnyConnect Split Tunnel - Running as admin")
}
