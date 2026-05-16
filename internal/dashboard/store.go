package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Action string

const (
	ActionDisconnect      Action = "disconnect"
	ActionReconnect       Action = "reconnect"
	ActionCodexMode       Action = "codex_mode"
	ActionRestoreNormal   Action = "restore_normal"
	ActionToggleSplit     Action = "toggle_split"
	ActionUpdateIPDB      Action = "update_ipdb"
	ActionViewLog         Action = "view_log"
	ActionToggleAutoStart Action = "toggle_autostart"
	ActionContactAuthor   Action = "contact_author"
	ActionQuit            Action = "quit"
)

type Snapshot struct {
	StatusText          string    `json:"status_text"`
	CurrentSite         string    `json:"current_site"`
	SplitTunnelEnabled  bool      `json:"split_tunnel_enabled"`
	AutoStartEnabled    bool      `json:"auto_start_enabled"`
	Backend             string    `json:"backend"`
	RouteCount          int       `json:"route_count"`
	LastIPDBUpdate      time.Time `json:"last_ipdb_update"`
	OriginalGateway     string    `json:"original_gateway"`
	OriginalInterface   int       `json:"original_interface"`
	OriginalIPv6Gateway string    `json:"original_ipv6_gateway"`
	OriginalIPv6IfIndex int       `json:"original_ipv6_interface_index"`
	IPv6SplitEnabled    bool      `json:"ipv6_split_enabled"`
	LastError           string    `json:"last_error"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type Command struct {
	Action    Action    `json:"action"`
	Enabled   *bool     `json:"enabled,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	dir string
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) SnapshotPath() string {
	return filepath.Join(s.dir, "dashboard-state.json")
}

func (s *Store) CommandDir() string {
	return filepath.Join(s.dir, "dashboard-commands")
}

func (s *Store) WriteSnapshot(snapshot Snapshot) error {
	if snapshot.UpdatedAt.IsZero() {
		snapshot.UpdatedAt = time.Now()
	}
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.SnapshotPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.SnapshotPath())
}

func (s *Store) WriteCommand(command Command) error {
	if err := command.Validate(); err != nil {
		return err
	}
	if command.CreatedAt.IsZero() {
		command.CreatedAt = time.Now()
	}
	if err := os.MkdirAll(s.CommandDir(), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(command, "", "  ")
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%d-%s.json", command.CreatedAt.UnixNano(), command.Action)
	return os.WriteFile(filepath.Join(s.CommandDir(), name), data, 0644)
}

func (s *Store) ConsumeCommands() ([]Command, error) {
	entries, err := os.ReadDir(s.CommandDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var commands []Command
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		path := filepath.Join(s.CommandDir(), entry.Name())
		data, readErr := os.ReadFile(path)
		removeErr := os.Remove(path)
		if readErr != nil {
			if removeErr != nil {
				return commands, removeErr
			}
			continue
		}
		var command Command
		if err := json.Unmarshal(data, &command); err != nil {
			if removeErr != nil {
				return commands, removeErr
			}
			continue
		}
		if err := command.Validate(); err != nil {
			if removeErr != nil {
				return commands, removeErr
			}
			continue
		}
		commands = append(commands, command)
		if removeErr != nil {
			return commands, removeErr
		}
	}
	return commands, nil
}

func (c Command) Validate() error {
	switch c.Action {
	case ActionDisconnect, ActionReconnect, ActionCodexMode, ActionRestoreNormal,
		ActionUpdateIPDB, ActionViewLog, ActionContactAuthor, ActionQuit:
		return nil
	case ActionToggleSplit, ActionToggleAutoStart:
		if c.Enabled == nil {
			return fmt.Errorf("%s command requires enabled value", c.Action)
		}
		return nil
	default:
		return fmt.Errorf("unknown dashboard action: %s", c.Action)
	}
}
