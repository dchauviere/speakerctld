package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/dchauviere/speakerctld/internal/state"
)

type shutdownOrchestrator struct {
	state.StateMachine
	synced chan struct{}
	closed chan struct{}
}

func TestStartStopsOnSIGTERM(t *testing.T) {
	if os.Getenv("SPEAKERCTLD_SHUTDOWN_TEST") == "1" {
		o := &shutdownOrchestrator{synced: make(chan struct{}), closed: make(chan struct{})}
		app := &SpeakerCtld{orchestrator: o, doneChan: make(chan struct{})}
		app.Start()
		<-o.synced
		fmt.Println("ready")
		<-app.doneChan
		select {
		case <-o.closed:
		default:
			t.Fatal("orchestrator was not closed")
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestStartStopsOnSIGTERM$")
	cmd.Env = append(os.Environ(), "SPEAKERCTLD_SHUTDOWN_TEST=1")
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		ready <- scanner.Scan() && scanner.Text() == "ready"
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("child did not report readiness")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not start")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child did not exit cleanly: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatal("child did not stop after SIGTERM")
	}
}

func (o *shutdownOrchestrator) CurrentState() state.State { return state.Online }
func (o *shutdownOrchestrator) IsProvisioned() bool       { return true }
func (o *shutdownOrchestrator) TransitionTo(state.State) error {
	close(o.synced)
	return nil
}
func (o *shutdownOrchestrator) Close() { close(o.closed) }

func TestRunStopsAndCleansUpOnCancellation(t *testing.T) {
	o := &shutdownOrchestrator{synced: make(chan struct{}), closed: make(chan struct{})}
	app := &SpeakerCtld{orchestrator: o, doneChan: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- app.run(ctx) }()

	select {
	case <-o.synced:
	case <-time.After(time.Second):
		t.Fatal("startup did not complete")
	}
	select {
	case <-finished:
		t.Fatal("run returned before cancellation")
	default:
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("run failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not stop after cancellation")
	}
	select {
	case <-o.closed:
	default:
		t.Fatal("orchestrator was not closed")
	}
}
