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

	"github.com/user/anyconnect-split/internal/config"
)

type Action string

const (
	ActionDisconnect          Action = "disconnect"
	ActionReconnect           Action = "reconnect"
	ActionSelectSite          Action = "select_site"
	ActionCodexMode           Action = "codex_mode"
	ActionRestoreNormal       Action = "restore_normal"
	ActionSetSplitMode        Action = "set_split_mode"
	ActionUpdateIPDB          Action = "update_ipdb"
	ActionAddForeignDomain    Action = "add_foreign_domain"
	ActionAddForeignCIDR      Action = "add_foreign_cidr"
	ActionRemoveForeignDomain Action = "remove_foreign_domain"
	ActionRemoveForeignCIDR   Action = "remove_foreign_cidr"
	ActionSmartContinue       Action = "smart_select_continue"
	ActionSmartCancel         Action = "smart_select_cancel"
	ActionSmartAccept         Action = "smart_select_accept"
	ActionSmartRestore        Action = "smart_select_restore"
	ActionViewLog             Action = "view_log"
	ActionToggleAutoStart     Action = "toggle_autostart"
	ActionContactAuthor       Action = "contact_author"
	ActionQuit                Action = "quit"
)

type Snapshot struct {
	Sites               []string  `json:"sites"`
	ConnectionBusy      bool      `json:"connection_busy"`
	StatusText          string    `json:"status_text"`
	CurrentSite         string    `json:"current_site"`
	SplitTunnelEnabled  bool      `json:"split_tunnel_enabled"`
	SplitMode           string    `json:"split_mode"`
	AutoStartEnabled    bool      `json:"auto_start_enabled"`
	Backend             string    `json:"backend"`
	RouteCount          int       `json:"route_count"`
	CodexModeActive     bool      `json:"codex_mode_active"`
	LastIPDBUpdate      time.Time `json:"last_ipdb_update"`
	OriginalGateway     string    `json:"original_gateway"`
	OriginalInterface   int       `json:"original_interface"`
	OriginalIPv6Gateway string    `json:"original_ipv6_gateway"`
	OriginalIPv6IfIndex int       `json:"original_ipv6_interface_index"`
	IPv6SplitEnabled    bool      `json:"ipv6_split_enabled"`
	LastError           string    `json:"last_error"`
	ForeignDomains      []string  `json:"foreign_domains"`
	ForeignCIDRs        []string  `json:"foreign_cidrs"`
	SmartState          string    `json:"smart_state"`
	SmartMessage        string    `json:"smart_message"`
	SmartResultID       string    `json:"smart_result_id"`
	SmartCandidate      string    `json:"smart_candidate"`
	SmartAttempts       int       `json:"smart_attempts"`
	SmartSuccesses      int       `json:"smart_successes"`
	SmartMedianMS       int64     `json:"smart_median_ms"`
	SmartSlowestMS      int64     `json:"smart_slowest_ms"`
	SmartExitIP         string    `json:"smart_exit_ip"`
	SmartExitRegion     string    `json:"smart_exit_region"`
	SmartDeadline       time.Time `json:"smart_deadline"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type Command struct {
	Action    Action    `json:"action"`
	Enabled   *bool     `json:"enabled,omitempty"`
	Value     *string   `json:"value,omitempty"`
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
		ActionSmartContinue, ActionSmartCancel, ActionSmartAccept, ActionSmartRestore,
		ActionUpdateIPDB, ActionViewLog, ActionContactAuthor, ActionQuit:
		return nil
	case ActionToggleAutoStart:
		if c.Enabled == nil {
			return fmt.Errorf("%s command requires enabled value", c.Action)
		}
		return nil
	case ActionSetSplitMode:
		if c.Value == nil {
			return fmt.Errorf("%s command requires value", c.Action)
		}
		if _, ok := config.NormalizeSplitMode(*c.Value); !ok {
			return fmt.Errorf("%s command has unsupported value %q", c.Action, *c.Value)
		}
		return nil
	case ActionSelectSite, ActionAddForeignDomain, ActionAddForeignCIDR, ActionRemoveForeignDomain, ActionRemoveForeignCIDR:
		if c.Value == nil || strings.TrimSpace(*c.Value) == "" {
			return fmt.Errorf("%s command requires value", c.Action)
		}
		return nil
	default:
		return fmt.Errorf("unknown dashboard action: %s", c.Action)
	}
}
