package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dchauviere/speakerctld/internal/state"
	"github.com/dchauviere/speakerctld/internal/storage"
)

type shutdownOrchestrator struct {
	state.StateMachine
	synced chan struct{}
	closed chan struct{}
}

type testOrchestrator struct {
	state.StateMachine
	transitions []state.State
}

func (o *testOrchestrator) TransitionTo(target state.State) error {
	o.transitions = append(o.transitions, target)
	return o.Transition(target)
}

func (o *testOrchestrator) Close() {}

func TestPortalWaitsForNewWifiSubmission(t *testing.T) {
	st := storage.NewFileStorage(t.TempDir())
	if err := st.SaveSystemState(&storage.SystemState{State: string(state.WifiConnect), Provisioned: true}); err != nil {
		t.Fatal(err)
	}
	sm, err := state.NewStateMachine(st)
	if err != nil {
		t.Fatal(err)
	}
	o := &testOrchestrator{StateMachine: sm}
	app := &SpeakerCtld{orchestrator: o, storage: st}
	for i := 0; i < 3; i++ {
		app.handleWifiConnectState(true, false)
	}
	if o.CurrentState() != state.FirstBootAP {
		t.Fatalf("expected captive portal, got %s", o.CurrentState())
	}
	app.handleFirstBootAPState(true, false)
	if len(o.transitions) != 0 {
		t.Fatal("portal retried old credentials without user action")
	}
	if err := o.ResetWifiFailures(); err != nil {
		t.Fatal(err)
	}
	app.handleFirstBootAPState(true, false)
	if o.CurrentState() != state.WifiConnect || len(o.transitions) != 1 {
		t.Fatal("portal did not retry after WiFi submission")
	}
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

type failedStartupOrchestrator struct {
	*shutdownOrchestrator
}

func (o *failedStartupOrchestrator) TransitionTo(state.State) error {
	return errors.New("access point unavailable")
}

func TestRunFailsWhenInitialSetupFails(t *testing.T) {
	o := &failedStartupOrchestrator{&shutdownOrchestrator{closed: make(chan struct{})}}
	app := &SpeakerCtld{orchestrator: o}
	err := app.run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "access point unavailable") {
		t.Fatalf("expected startup error, got %v", err)
	}
	select {
	case <-o.closed:
	default:
		t.Fatal("orchestrator was not closed on startup failure")
	}
}

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
