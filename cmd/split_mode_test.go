package main

import (
	"errors"
	"testing"

	"github.com/user/anyconnect-split/internal/config"
)

func TestPersistSplitModeSavesChangedMode(t *testing.T) {
	cfg := &config.Config{SplitMode: config.SplitModeDomesticDirect}
	saves := 0

	changed, err := persistSplitMode(cfg, config.SplitModeForeignDirect, func() error {
		saves++
		return nil
	})

	if err != nil || !changed || saves != 1 || cfg.SplitMode != config.SplitModeForeignDirect {
		t.Fatalf("changed=%v saves=%d mode=%q err=%v", changed, saves, cfg.SplitMode, err)
	}
}

func TestPersistSplitModeSkipsUnchangedMode(t *testing.T) {
	cfg := &config.Config{SplitMode: config.SplitModeDomesticDirect}
	saves := 0

	changed, err := persistSplitMode(cfg, " DOMESTIC_DIRECT ", func() error {
		saves++
		return nil
	})

	if err != nil || changed || saves != 0 || cfg.SplitMode != config.SplitModeDomesticDirect {
		t.Fatalf("changed=%v saves=%d mode=%q err=%v", changed, saves, cfg.SplitMode, err)
	}
}

func TestPersistSplitModeRollsBackWhenSaveFails(t *testing.T) {
	cfg := &config.Config{SplitMode: config.SplitModeDomesticDirect}

	changed, err := persistSplitMode(cfg, config.SplitModeForeignDirect, func() error {
		return errors.New("disk full")
	})

	if err == nil || changed || cfg.SplitMode != config.SplitModeDomesticDirect {
		t.Fatalf("changed=%v mode=%q err=%v", changed, cfg.SplitMode, err)
	}
}

func TestPersistSplitModeRejectsUnknownMode(t *testing.T) {
	cfg := &config.Config{SplitMode: config.SplitModeDomesticDirect}

	changed, err := persistSplitMode(cfg, "unknown", func() error {
		t.Fatal("save must not run for an invalid mode")
		return nil
	})

	if err == nil || changed || cfg.SplitMode != config.SplitModeDomesticDirect {
		t.Fatalf("changed=%v mode=%q err=%v", changed, cfg.SplitMode, err)
	}
}
