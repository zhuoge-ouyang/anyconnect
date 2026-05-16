package dashboard

import (
	"encoding/json"
	"os"
	"time"
)

type Actions struct {
	OnDisconnect      func()
	OnReconnect       func()
	OnCodexMode       func()
	OnRestoreNormal   func()
	OnToggleSplit     func(enabled bool)
	OnUpdateIPDB      func()
	OnViewLog         func()
	OnToggleAutoStart func(enabled bool)
	OnContactAuthor   func()
	OnQuit            func()
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
	case ActionCodexMode:
		if c.actions.OnCodexMode != nil {
			c.actions.OnCodexMode()
		}
	case ActionRestoreNormal:
		if c.actions.OnRestoreNormal != nil {
			c.actions.OnRestoreNormal()
		}
	case ActionToggleSplit:
		if c.actions.OnToggleSplit != nil && command.Enabled != nil {
			c.actions.OnToggleSplit(*command.Enabled)
		}
	case ActionUpdateIPDB:
		if c.actions.OnUpdateIPDB != nil {
			c.actions.OnUpdateIPDB()
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
