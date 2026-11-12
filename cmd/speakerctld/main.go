package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/david/speakerctld/internal/state"
	"github.com/david/speakerctld/internal/storage"
)

// SpeakerCtld is the main application that manages the Speakerctld speaker network state.
type SpeakerCtld struct {
	stateMachine state.StateMachine
	storage      storage.Storage
}

// NewSpeakerCtld creates a new instance of the speaker controller.
func NewSpeakerCtld() (*SpeakerCtld, error) {
	// Initialize storage
	// In production, you might want to use a different storage backend
	// or configure the data directory based on environment variables
	st := storage.DefaultFileStorage()

	// Initialize state machine
	sm, err := state.NewStateMachine(st)
	if err != nil {
		return nil, err
	}

	return &SpeakerCtld{
		stateMachine: sm,
		storage:      st,
	}, nil
}

// Run starts the main application loop.
func (s *SpeakerCtld) Run() error {
	// Log initial state
	log.Printf("Starting speakerctld, current state: %s", s.stateMachine.CurrentState())
	log.Printf("Device provisioned: %v", s.stateMachine.IsProvisioned())

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Main loop - in a real implementation, this would monitor network state
	// and trigger transitions based on connectivity
	<-sigChan

	log.Println("Shutting down...")
	return nil
}

func main() {
	log.Println("Initializing Speakerctld Speaker Controller...")

	// Initialize the application
	app, err := NewSpeakerCtld()
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}

	// Run the application
	if err := app.Run(); err != nil {
		log.Fatalf("Application error: %v", err)
	}

	log.Println("Speakerctld Speaker Controller stopped")
}
