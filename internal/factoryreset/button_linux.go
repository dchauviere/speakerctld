package factoryreset

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
	"unsafe"

	"github.com/godbus/dbus/v5"
	"golang.org/x/sys/unix"
)

const (
	keyPower   = 116
	synReport  = 0
	synDropped = 3
)

type inputEvent struct {
	Time  unix.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

type holdDetector struct {
	started time.Time
	dropped bool
}

func (d *holdDetector) event(typ, code uint16, value int32, now time.Time) {
	if typ == unix.EV_SYN && code == synDropped {
		d.started = time.Time{}
		d.dropped = true
		return
	}
	if d.dropped {
		if typ == unix.EV_SYN && code == synReport {
			d.dropped = false
		}
		return
	}
	if typ != unix.EV_KEY || code != keyPower {
		return
	}
	switch value {
	case 0:
		d.started = time.Time{}
	case 1:
		if d.started.IsZero() {
			d.started = now
		}
	}
}

func (d *holdDetector) ready(now time.Time, hold time.Duration) bool {
	return !d.started.IsZero() && now.Sub(d.started) >= hold
}

// WatchButton monitors the configured Linux input device, retrying after errors.
func WatchButton(ctx context.Context, path string, hold time.Duration, request func() error) {
	for ctx.Err() == nil {
		if err := watchButton(ctx, path, hold, request); err != nil && ctx.Err() == nil {
			log.Printf("Factory reset button unavailable: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func watchButton(ctx context.Context, path string, hold time.Duration, request func() error) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer unix.Close(fd)

	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return fmt.Errorf("connect to logind: %w", err)
	}
	defer conn.Close()
	var inhibitor dbus.UnixFD
	inhibitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	call := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1").
		CallWithContext(inhibitCtx, "org.freedesktop.login1.Manager.Inhibit", 0, "handle-power-key", "speakerctld", "factory reset button", "block")
	if call.Err != nil {
		return fmt.Errorf("inhibit logind power key: %w", call.Err)
	}
	if err := call.Store(&inhibitor); err != nil {
		return fmt.Errorf("read power key inhibitor: %w", err)
	}
	defer unix.Close(int(inhibitor))
	log.Printf("Monitoring factory reset button %s (hold %s)", path, hold)

	return monitorInput(ctx, fd, hold, request)
}

func monitorInput(ctx context.Context, fd int, hold time.Duration, request func() error) error {
	var detector holdDetector
	buf := make([]byte, int(unsafe.Sizeof(inputEvent{}))*32)
	for ctx.Err() == nil {
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		_, err := unix.Poll(poll, 100)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return fmt.Errorf("poll input device: %w", err)
		}
		if poll[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return errors.New("input device disconnected")
		}
		if poll[0].Revents&unix.POLLIN != 0 {
			for {
				n, err := unix.Read(fd, buf)
				if errors.Is(err, unix.EAGAIN) {
					break
				}
				if err != nil {
					return fmt.Errorf("read input device: %w", err)
				}
				if n == 0 {
					return errors.New("input device closed")
				}
				size := int(unsafe.Sizeof(inputEvent{}))
				if n%size != 0 {
					return errors.New("incomplete input event")
				}
				for offset := 0; offset < n; offset += size {
					// The kernel input_event uses native byte order and timeval size.
					event := *(*inputEvent)(unsafe.Pointer(&buf[offset]))
					detector.event(event.Type, event.Code, event.Value, time.Now())
				}
			}
		}
		if ctx.Err() == nil && detector.ready(time.Now(), hold) {
			if err := request(); err != nil {
				return fmt.Errorf("request factory reset: %w", err)
			}
			return nil
		}
	}
	return nil
}
