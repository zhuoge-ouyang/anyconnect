package tray

import (
	"sync"
	"time"

	"github.com/getlantern/systray"
)

type trayIconMode int

const (
	trayIconModeIdle trayIconMode = iota
	trayIconModeActive
	trayIconModeBusy
	trayIconModeError
)

type trayIconAnimation struct {
	frames   [][]byte
	interval time.Duration
}

func trayIconAnimationFor(mode trayIconMode) trayIconAnimation {
	switch mode {
	case trayIconModeActive:
		return trayIconAnimation{frames: iconActiveFrames, interval: 850 * time.Millisecond}
	case trayIconModeBusy:
		return trayIconAnimation{frames: iconBusyFrames, interval: 140 * time.Millisecond}
	case trayIconModeError:
		return trayIconAnimation{frames: [][]byte{iconError}}
	case trayIconModeIdle:
		return trayIconAnimation{frames: [][]byte{iconIdle}}
	default:
		return trayIconAnimation{frames: [][]byte{iconIdle}}
	}
}

type trayIconAnimator struct {
	mu     sync.Mutex
	stop   chan struct{}
	setter func([]byte)
}

func newTrayIconAnimator(setter func([]byte)) *trayIconAnimator {
	if setter == nil {
		setter = systray.SetIcon
	}
	return &trayIconAnimator{setter: setter}
}

func (t *Tray) setTrayIconMode(mode trayIconMode) {
	if t.iconAnimator == nil {
		t.iconAnimator = newTrayIconAnimator(nil)
	}
	t.iconAnimator.apply(trayIconAnimationFor(mode))
}

func (t *Tray) stopTrayIconAnimation() {
	if t.iconAnimator != nil {
		t.iconAnimator.stopAnimation()
	}
}

func (a *trayIconAnimator) apply(animation trayIconAnimation) {
	a.mu.Lock()
	a.stopLocked()
	if len(animation.frames) == 0 {
		a.mu.Unlock()
		return
	}
	a.setter(animation.frames[0])
	if len(animation.frames) == 1 || animation.interval <= 0 {
		a.mu.Unlock()
		return
	}

	stop := make(chan struct{})
	a.stop = stop
	frames := append([][]byte(nil), animation.frames...)
	interval := animation.interval
	setter := a.setter
	a.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		index := 1
		for {
			select {
			case <-ticker.C:
				setter(frames[index])
				index = (index + 1) % len(frames)
			case <-stop:
				return
			}
		}
	}()
}

func (a *trayIconAnimator) stopAnimation() {
	a.mu.Lock()
	a.stopLocked()
	a.mu.Unlock()
}

func (a *trayIconAnimator) stopLocked() {
	if a.stop == nil {
		return
	}
	close(a.stop)
	a.stop = nil
}
