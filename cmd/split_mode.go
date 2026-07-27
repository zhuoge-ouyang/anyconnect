package main

import (
	"fmt"

	"github.com/user/anyconnect-split/internal/config"
)

func persistSplitMode(cfg *config.Config, target string, save func() error) (bool, error) {
	normalized, ok := config.NormalizeSplitMode(target)
	if !ok {
		return false, fmt.Errorf("unsupported split mode %q", target)
	}
	if cfg.SplitMode == normalized {
		return false, nil
	}

	previous := cfg.SplitMode
	cfg.SplitMode = normalized
	if err := save(); err != nil {
		cfg.SplitMode = previous
		return false, fmt.Errorf("save split mode: %w", err)
	}
	return true, nil
}
