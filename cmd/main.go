package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/user/anyconnect-split/internal/config"
	"github.com/user/anyconnect-split/internal/ipdb"
	"github.com/user/anyconnect-split/internal/monitor"
	"github.com/user/anyconnect-split/internal/route"
	"github.com/user/anyconnect-split/internal/tray"
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

func baseDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

func main() {
	if !isAdmin() {
		// Try to relaunch as admin using ShellExecute runas
		exe, _ := os.Executable()
		verb := "runas"
		verbPtr, _ := windows.UTF16PtrFromString(verb)
		exePtr, _ := windows.UTF16PtrFromString(exe)
		cwdPtr, _ := windows.UTF16PtrFromString(".")
		windows.ShellExecute(0, verbPtr, exePtr, nil, cwdPtr, windows.SW_NORMAL)
		os.Exit(0)
	}

	// Setup logging
	logFile := filepath.Join(baseDir(), "split-tunnel.log")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(f)
		defer f.Close()
	}
	log.Println("AnyConnect Split Tunnel starting...")

	// Load config
	cfg, err := config.Load()
	if err != nil {
		log.Printf("Warning: failed to load config, using defaults: %v", err)
		cfg = config.DefaultConfig()
	}

	// Detect original gateway
	if cfg.OriginalGateway == "" {
		gw, err := monitor.GetDefaultGateway()
		if err != nil {
			log.Printf("Warning: could not detect default gateway: %v", err)
		} else {
			cfg.OriginalGateway = gw
			cfg.Save()
			log.Printf("Detected original gateway: %s", gw)
		}
	}

	// Initialize IP database
	dataDir := filepath.Join(baseDir(), "data")
	db := ipdb.New(dataDir)

	// Check if IP list needs downloading
	if db.NeedsUpdate() {
		log.Println("IP database empty, downloading...")
		if _, err := db.Update(); err != nil {
			log.Printf("Failed to download IP database: %v", err)
		} else {
			cfg.LastUpdate = time.Now()
			cfg.Save()
			log.Println("IP database downloaded successfully")
		}
	}

	// Initialize route manager
	routeMgr := route.NewManager(cfg.OriginalGateway, dataDir)

	// Cleanup stale routes from previous crash
	routeMgr.CleanupStaleRoutes()

	// Initialize VPN monitor
	vpnMon := monitor.New()

	// Define tray actions
	actions := tray.Actions{
		OnToggleSplit: func(enabled bool) {
			cfg.SplitTunnelEnabled = enabled
			cfg.Save()
			if !enabled && routeMgr.HasAppliedRoutes() {
				routeMgr.RemoveAllRoutes()
				log.Println("Split tunnel disabled, routes removed")
			}
		},
		OnUpdateIPDB: func() {
			log.Println("Manual IP database update requested")
			if _, err := db.Update(); err != nil {
				log.Printf("Update failed: %v", err)
			} else {
				cfg.LastUpdate = time.Now()
				cfg.Save()
				log.Println("IP database updated successfully")
			}
		},
		OnViewLog: func() {
			exec.Command("notepad", logFile).Start()
		},
		OnToggleAuto: func(enabled bool) {
			cfg.AutoStart = enabled
			cfg.Save()
			setAutoStart(enabled)
		},
		OnQuit: func() {
			log.Println("Quitting, cleaning up routes...")
			routeMgr.RemoveAllRoutes()
			vpnMon.Stop()
			cfg.Save()
		},
	}

	// Start VPN monitor
	vpnMon.Start()

	// Create tray UI
	trayUI := tray.New(actions, cfg.SplitTunnelEnabled, cfg.AutoStart)

	// Handle VPN state changes in background
	go func() {
		for change := range vpnMon.StateChanges() {
			switch change.NewState {
			case monitor.StateConnected:
				log.Println("VPN connected detected")
				if !cfg.SplitTunnelEnabled {
					vpnMon.SetState(monitor.StateActive)
					continue
				}
				trayUI.SetStatusBusy("Applying routes...")

				if cfg.OriginalGateway == "" {
					log.Println("Error: no original gateway configured")
					trayUI.SetStatusError("No gateway configured")
					continue
				}

				cidrs, err := db.Load()
				if err != nil {
					log.Printf("Error loading IP list: %v", err)
					trayUI.SetStatusError("Failed to load IP list")
					continue
				}

				added, errors := routeMgr.AddRoutes(cidrs)
				log.Printf("Routes applied: %d added, %d errors", added, errors)
				trayUI.SetStatusActive(added)
				vpnMon.SetState(monitor.StateActive)

			case monitor.StateCleaning:
				log.Println("VPN disconnected detected, cleaning routes...")
				trayUI.SetStatusBusy("Cleaning routes...")
				removed, _ := routeMgr.RemoveAllRoutes()
				log.Printf("Routes cleaned: %d removed", removed)
				trayUI.SetStatusIdle()
				vpnMon.SetState(monitor.StateIdle)
			}
		}
	}()

	// Auto-update IP database check
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if time.Since(cfg.LastUpdate) > time.Duration(cfg.UpdateIntervalDays)*24*time.Hour {
				log.Println("Auto-updating IP database...")
				if _, err := db.Update(); err != nil {
					log.Printf("Auto-update failed: %v", err)
				} else {
					cfg.LastUpdate = time.Now()
					cfg.Save()
					log.Println("Auto-update completed")
				}
			}
		}
	}()

	// Run tray (blocks until quit)
	trayUI.Run()
}

func setAutoStart(enabled bool) {
	exe, _ := os.Executable()
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Run`,
		registry.SET_VALUE)
	if err != nil {
		log.Printf("Failed to open registry: %v", err)
		return
	}
	defer k.Close()

	if enabled {
		err = k.SetStringValue("AnyConnectSplit", exe)
		if err != nil {
			log.Printf("Failed to set registry value: %v", err)
		}
	} else {
		err = k.DeleteValue("AnyConnectSplit")
		if err != nil {
			log.Printf("Failed to delete registry value: %v", err)
		}
	}
}
