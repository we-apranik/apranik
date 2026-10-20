package muxcool

import (
	"sync"
	"time"
)

type deadline struct {
	mutex  sync.Mutex
	timer  *time.Timer
	cancel chan struct{}
}

func newDeadline() deadline {
	return deadline{cancel: make(chan struct{})}
}

func (d *deadline) set(at time.Time) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.timer != nil && !d.timer.Stop() {
		<-d.cancel
	}
	d.timer = nil

	passed := isClosed(d.cancel)
	if at.IsZero() {
		if passed {
			d.cancel = make(chan struct{})
		}
		return
	}

	if remaining := time.Until(at); remaining > 0 {
		if passed {
			d.cancel = make(chan struct{})
		}
		cancel := d.cancel
		d.timer = time.AfterFunc(remaining, func() { close(cancel) })
		return
	}

	if !passed {
		close(d.cancel)
	}
}

func (d *deadline) wait() <-chan struct{} {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.cancel
}

func isClosed(channel <-chan struct{}) bool {
	select {
	case <-channel:
		return true
	default:
		return false
	}
}
