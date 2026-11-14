package swupdate

import (
	"testing"
	"time"
)

func TestCloseClosesSubscribers(t *testing.T) {
	installer := &simpleInstaller{closeChan: make(chan struct{})}
	events, unsubscribe := installer.Subscribe()
	defer unsubscribe()

	done := make(chan error, 1)
	go func() { done <- installer.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("close failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not return")
	}
	if _, ok := <-events; ok {
		t.Fatal("subscriber channel remains open")
	}
	lateEvents, lateUnsubscribe := installer.Subscribe()
	defer lateUnsubscribe()
	if _, ok := <-lateEvents; ok {
		t.Fatal("subscriber registered after close remains open")
	}
}

func TestNotifyConcurrentWithShutdown(t *testing.T) {
	for _, action := range []string{"unsubscribe", "close"} {
		t.Run(action, func(t *testing.T) {
			installer := &simpleInstaller{closeChan: make(chan struct{})}
			_, unsubscribe := installer.Subscribe()
			done := make(chan struct{})
			go func() {
				defer close(done)
				for i := 0; i < 100; i++ {
					installer.notifySubscribers(UpdateEvent{Type: "status"})
				}
			}()
			if action == "unsubscribe" {
				unsubscribe()
			}
			if err := installer.Close(); err != nil {
				t.Fatal(err)
			}
			<-done
			unsubscribe()
		})
	}
}
