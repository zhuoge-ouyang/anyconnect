package tray

import (
	"encoding/binary"
	"testing"
)

func TestGeneratedTrayIconsUseMultipleSizes(t *testing.T) {
	tests := []struct {
		name string
		icon []byte
	}{
		{name: "idle", icon: iconIdle},
		{name: "active", icon: iconActive},
		{name: "busy", icon: iconBusy},
		{name: "error", icon: iconError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := icoImageCount(t, tt.icon)
			if count < 4 {
				t.Fatalf("%s icon has %d image(s), want at least 4 sizes for sharp tray rendering", tt.name, count)
			}
		})
	}
}

func TestTrayIconModesUseStaticFrames(t *testing.T) {
	tests := []struct {
		name string
		mode trayIconMode
	}{
		{name: "idle", mode: trayIconModeIdle},
		{name: "active", mode: trayIconModeActive},
		{name: "busy", mode: trayIconModeBusy},
		{name: "error", mode: trayIconModeError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			animation := trayIconAnimationFor(tt.mode)
			if len(animation.frames) != 1 {
				t.Fatalf("%s icon mode has %d frame(s), want exactly 1", tt.name, len(animation.frames))
			}
			if animation.interval != 0 {
				t.Fatalf("%s static icon interval = %s, want 0", tt.name, animation.interval)
			}
		})
	}
}

func icoImageCount(t *testing.T, icon []byte) int {
	t.Helper()
	if len(icon) < 6 {
		t.Fatalf("ICO too short: %d bytes", len(icon))
	}
	if binary.LittleEndian.Uint16(icon[0:2]) != 0 || binary.LittleEndian.Uint16(icon[2:4]) != 1 {
		t.Fatalf("invalid ICO header")
	}
	return int(binary.LittleEndian.Uint16(icon[4:6]))
}
