package tray

import (
	"fmt"

	"github.com/getlantern/systray"
)

type Actions struct {
	OnToggleSplit func(enabled bool)
	OnUpdateIPDB  func()
	OnViewLog     func()
	OnToggleAuto  func(enabled bool)
	OnQuit        func()
}

type Tray struct {
	actions       Actions
	menuStatus    *systray.MenuItem
	menuToggle    *systray.MenuItem
	menuUpdate    *systray.MenuItem
	menuLog       *systray.MenuItem
	menuAutoStart *systray.MenuItem
	menuQuit      *systray.MenuItem
	splitEnabled  bool
	autoStart     bool
}

func New(actions Actions, splitEnabled bool, autoStart bool) *Tray {
	return &Tray{
		actions:      actions,
		splitEnabled: splitEnabled,
		autoStart:    autoStart,
	}
}

func (t *Tray) Run() {
	systray.Run(t.onReady, t.onExit)
}

func (t *Tray) onReady() {
	systray.SetIcon(iconIdle)
	systray.SetTitle("AnyConnect Split")
	systray.SetTooltip("AnyConnect Split Tunnel - Idle")

	t.menuStatus = systray.AddMenuItem("Status: VPN not connected", "")
	t.menuStatus.Disable()
	systray.AddSeparator()

	t.menuToggle = systray.AddMenuItemCheckbox("Enable Split Tunnel", "Toggle split tunneling", t.splitEnabled)
	t.menuUpdate = systray.AddMenuItem("Update IP Database", "Download latest China IP list")
	t.menuLog = systray.AddMenuItem("View Log", "Open log file")
	systray.AddSeparator()

	t.menuAutoStart = systray.AddMenuItemCheckbox("Start with Windows", "Auto-start on login", t.autoStart)
	t.menuQuit = systray.AddMenuItem("Quit", "Exit application")

	go t.handleClicks()
}

func (t *Tray) onExit() {}

func (t *Tray) handleClicks() {
	for {
		select {
		case <-t.menuToggle.ClickedCh:
			t.splitEnabled = !t.splitEnabled
			if t.splitEnabled {
				t.menuToggle.Check()
			} else {
				t.menuToggle.Uncheck()
			}
			if t.actions.OnToggleSplit != nil {
				t.actions.OnToggleSplit(t.splitEnabled)
			}
		case <-t.menuUpdate.ClickedCh:
			if t.actions.OnUpdateIPDB != nil {
				t.actions.OnUpdateIPDB()
			}
		case <-t.menuLog.ClickedCh:
			if t.actions.OnViewLog != nil {
				t.actions.OnViewLog()
			}
		case <-t.menuAutoStart.ClickedCh:
			t.autoStart = !t.autoStart
			if t.autoStart {
				t.menuAutoStart.Check()
			} else {
				t.menuAutoStart.Uncheck()
			}
			if t.actions.OnToggleAuto != nil {
				t.actions.OnToggleAuto(t.autoStart)
			}
		case <-t.menuQuit.ClickedCh:
			if t.actions.OnQuit != nil {
				t.actions.OnQuit()
			}
			systray.Quit()
			return
		}
	}
}

func (t *Tray) SetStatusIdle() {
	systray.SetIcon(iconIdle)
	systray.SetTooltip("AnyConnect Split Tunnel - Idle")
	t.menuStatus.SetTitle("Status: VPN not connected")
}

func (t *Tray) SetStatusActive(routeCount int) {
	systray.SetIcon(iconActive)
	systray.SetTooltip("AnyConnect Split Tunnel - Active")
	t.menuStatus.SetTitle(fmt.Sprintf("Status: Split active (%d routes)", routeCount))
}

func (t *Tray) SetStatusBusy(msg string) {
	systray.SetIcon(iconBusy)
	systray.SetTooltip("AnyConnect Split Tunnel - Working...")
	t.menuStatus.SetTitle(fmt.Sprintf("Status: %s", msg))
}

func (t *Tray) SetStatusError(msg string) {
	systray.SetIcon(iconError)
	systray.SetTooltip("AnyConnect Split Tunnel - Error")
	t.menuStatus.SetTitle(fmt.Sprintf("Error: %s", msg))
}
