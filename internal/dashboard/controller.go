package dashboard

import (
	"encoding/json"
	"os"
	"time"
)

type Actions struct {
	OnDisconnect          func()
	OnReconnect           func()
	OnSelectSite          func(name string)
	OnCodexMode           func()
	OnRestoreNormal       func()
	OnSetSplitMode        func(mode string)
	OnUpdateIPDB          func()
	OnAddForeignDomain    func(value string)
	OnAddForeignCIDR      func(value string)
	OnRemoveForeignDomain func(value string)
	OnRemoveForeignCIDR   func(value string)
	OnSmartContinue       func()
	OnSmartCancel         func()
	OnSmartAccept         func()
	OnSmartRestore        func()
	OnViewLog             func()
	OnToggleAutoStart     func(enabled bool)
	OnContactAuthor       func()
	OnQuit                func()
}

type Controller struct {
	store   *Store
	actions Actions
}

func NewController(store *Store, actions Actions) *Controller {
	return &Controller{store: store, actions: actions}
}

func (c *Controller) UpdateSnapshot(snapshot Snapshot) error {
	if snapshot.UpdatedAt.IsZero() {
		snapshot.UpdatedAt = time.Now()
	}
	return c.store.WriteSnapshot(snapshot)
}

func (c *Controller) ReadSnapshot() (Snapshot, error) {
	data, err := os.ReadFile(c.store.SnapshotPath())
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func (c *Controller) ProcessPendingCommands() error {
	commands, err := c.store.ConsumeCommands()
	if err != nil {
		return err
	}
	for _, command := range commands {
		c.dispatch(command)
	}
	return nil
}

func (c *Controller) dispatch(command Command) {
	switch command.Action {
	case ActionDisconnect:
		if c.actions.OnDisconnect != nil {
			c.actions.OnDisconnect()
		}
	case ActionReconnect:
		if c.actions.OnReconnect != nil {
			c.actions.OnReconnect()
		}
	case ActionSelectSite:
		if c.actions.OnSelectSite != nil && command.Value != nil {
			c.actions.OnSelectSite(*command.Value)
		}
	case ActionCodexMode:
		if c.actions.OnCodexMode != nil {
			c.actions.OnCodexMode()
		}
	case ActionRestoreNormal:
		if c.actions.OnRestoreNormal != nil {
			c.actions.OnRestoreNormal()
		}
	case ActionSetSplitMode:
		if c.actions.OnSetSplitMode != nil && command.Value != nil {
			c.actions.OnSetSplitMode(*command.Value)
		}
	case ActionUpdateIPDB:
		if c.actions.OnUpdateIPDB != nil {
			c.actions.OnUpdateIPDB()
		}
	case ActionAddForeignDomain:
		if c.actions.OnAddForeignDomain != nil && command.Value != nil {
			c.actions.OnAddForeignDomain(*command.Value)
		}
	case ActionAddForeignCIDR:
		if c.actions.OnAddForeignCIDR != nil && command.Value != nil {
			c.actions.OnAddForeignCIDR(*command.Value)
		}
	case ActionRemoveForeignDomain:
		if c.actions.OnRemoveForeignDomain != nil && command.Value != nil {
			c.actions.OnRemoveForeignDomain(*command.Value)
		}
	case ActionRemoveForeignCIDR:
		if c.actions.OnRemoveForeignCIDR != nil && command.Value != nil {
			c.actions.OnRemoveForeignCIDR(*command.Value)
		}
	case ActionSmartContinue:
		if c.actions.OnSmartContinue != nil {
			c.actions.OnSmartContinue()
		}
	case ActionSmartCancel:
		if c.actions.OnSmartCancel != nil {
			c.actions.OnSmartCancel()
		}
	case ActionSmartAccept:
		if c.actions.OnSmartAccept != nil {
			c.actions.OnSmartAccept()
		}
	case ActionSmartRestore:
		if c.actions.OnSmartRestore != nil {
			c.actions.OnSmartRestore()
		}
	case ActionViewLog:
		if c.actions.OnViewLog != nil {
			c.actions.OnViewLog()
		}
	case ActionToggleAutoStart:
		if c.actions.OnToggleAutoStart != nil && command.Enabled != nil {
			c.actions.OnToggleAutoStart(*command.Enabled)
		}
	case ActionContactAuthor:
		if c.actions.OnContactAuthor != nil {
			c.actions.OnContactAuthor()
		}
	case ActionQuit:
		if c.actions.OnQuit != nil {
			c.actions.OnQuit()
		}
	}
}
