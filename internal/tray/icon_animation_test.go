package tray

import (
	"bytes"
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

func TestTrayIconAnimationsExposeExpectedFrames(t *testing.T) {
	tests := []struct {
		name      string
		mode      trayIconMode
		minFrames int
		animated  bool
	}{
		{name: "idle", mode: trayIconModeIdle, minFrames: 1, animated: false},
		{name: "active", mode: trayIconModeActive, minFrames: 4, animated: true},
		{name: "busy", mode: trayIconModeBusy, minFrames: 4, animated: true},
		{name: "error", mode: trayIconModeError, minFrames: 1, animated: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			animation := trayIconAnimationFor(tt.mode)
			if len(animation.frames) < tt.minFrames {
				t.Fatalf("%s animation has %d frame(s), want at least %d", tt.name, len(animation.frames), tt.minFrames)
			}
			if tt.animated && animation.interval <= 0 {
				t.Fatalf("%s animation interval = %s, want a positive duration", tt.name, animation.interval)
			}
			if !tt.animated && animation.interval != 0 {
				t.Fatalf("%s static icon interval = %s, want 0", tt.name, animation.interval)
			}
			if tt.animated && !containsDistinctFrames(animation.frames) {
				t.Fatalf("%s animation frames are identical", tt.name)
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

func containsDistinctFrames(frames [][]byte) bool {
	if len(frames) < 2 {
		return false
	}
	for _, frame := range frames[1:] {
		if !bytes.Equal(frames[0], frame) {
			return true
		}
	}
	return false
}
