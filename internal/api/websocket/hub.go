// Package websocket provides WebSocket connection management for real-time updates
package websocket

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Event represents a WebSocket event
type Event struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// Message represents a WebSocket message
type Message struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

// Hub maintains the set of active clients and broadcasts messages to the clients
type Hub struct {
	// Registered clients
	clients map[*Client]bool

	// Inbound messages from the clients
	broadcast chan Event

	// Register requests from the clients
	register chan *Client

	// Unregister requests from clients
	unregister chan *Client

	// Mutex for client map
	mutex sync.RWMutex

	// Upgrader for WebSocket connections
	upgrader websocket.Upgrader

	// Close channel
	closeChan chan struct{}
	closeDone chan struct{}
}

// NewHub creates a new WebSocket hub
func NewHub() *Hub {
	hub := &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan Event, 256),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		closeChan:  make(chan struct{}),
		closeDone:  make(chan struct{}),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for now
			},
		},
	}

	// Start the hub goroutine
	go hub.run()

	return hub
}

// run manages the hub's main loop
func (h *Hub) run() {
	defer close(h.closeDone)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-h.closeChan:
			// Close all clients
			h.mutex.RLock()
			for client := range h.clients {
				client.close()
			}
			h.mutex.RUnlock()
			return

		case client := <-h.register:
			h.mutex.Lock()
			h.clients[client] = true
			h.mutex.Unlock()

		case client := <-h.unregister:
			h.mutex.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mutex.Unlock()

		case event := <-h.broadcast:
			h.mutex.RLock()
			for client := range h.clients {
				select {
				case client.send <- event:
				default:
					// Client buffer full, close client
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mutex.RUnlock()

		case <-ticker.C:
			// Ping all clients to keep connections alive
			h.mutex.RLock()
			for client := range h.clients {
				client.ping()
			}
			h.mutex.RUnlock()
		}
	}
}

// Broadcast sends an event to all connected clients
func (h *Hub) Broadcast(event Event) {
	select {
	case h.broadcast <- event:
	case <-h.closeChan:
		// Hub is closing
	}
}

// BroadcastJSON sends a JSON-serializable payload as an event to all clients
func (h *Hub) BroadcastJSON(eventType string, payload interface{}) {
	event := Event{
		Type:    eventType,
		Payload: payload,
	}
	h.Broadcast(event)
}

// SendMessage sends a simple message to all clients
func (h *Hub) SendMessage(messageType, message string) {
	h.Broadcast(Event{
		Type: messageType,
		Payload: Message{
			Type:    messageType,
			Message: message,
		},
	})
}

// ClientCount returns the number of connected clients
func (h *Hub) ClientCount() int {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return len(h.clients)
}

// Close shuts down the hub and all connections
func (h *Hub) Close() error {
	close(h.closeChan)
	<-h.closeDone
	return nil
}

// Client is a middleman between the WebSocket connection and the hub
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan Event
}

// readPump pumps messages from the WebSocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}
		// We don't currently handle inbound messages
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case event, ok := <-c.send:
			if !ok {
				// Hub closed the send channel
				c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			
			// Marshal event to JSON
			jsonData, err := json.Marshal(event)
			if err != nil {
				log.Printf("Error marshaling event: %v", err)
				continue
			}
			
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				log.Printf("Error getting writer: %v", err)
				return
			}
			
			w.Write(jsonData)
			if err := w.Close(); err != nil {
				log.Printf("Error closing writer: %v", err)
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ping sends a ping message to the client
func (c *Client) ping() {
	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	c.conn.WriteMessage(websocket.PingMessage, nil)
}

// close closes the client connection
func (c *Client) close() {
	c.conn.Close()
}

// HandleWebSocket handles WebSocket connections
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	client := &Client{
		hub:  h,
		conn: conn,
		send: make(chan Event, 256),
	}

	h.register <- client

	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump()
}

// WebSocketHandler returns an http.Handler for WebSocket connections
type WebSocketHandler struct {
	hub *Hub
}

// ServeHTTP implements http.Handler for WebSocket connections
func (h *WebSocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.hub.HandleWebSocket(w, r)
}

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler(hub *Hub) *WebSocketHandler {
	return &WebSocketHandler{hub: hub}
}

// EventType constants
const (
	EventTypeProgress     = "progress"
	EventTypeStatus       = "status"
	EventTypeCompleted    = "completed"
	EventTypeFailed       = "failed"
	EventTypeStarted      = "started"
	EventTypeFirmwareInfo = "firmware_info"
	EventTypeSystemInfo   = "system_info"
)

// ProgressEvent represents a firmware update progress event
type ProgressEvent struct {
	Status         string `json:"status"`
	DownloadPercent int    `json:"download_percent"`
	DownloadBytes  int64  `json:"download_bytes"`
	TotalSteps     int    `json:"total_steps"`
	CurrentStep    int    `json:"current_step"`
	StepPercent    int    `json:"step_percent"`
	CurrentImage   string `json:"current_image"`
	HandlerName    string `json:"handler_name"`
	Message        string `json:"message"`
	Source         string `json:"source"`
}

// StatusEvent represents a status update event
type StatusEvent struct {
	Status string `json:"status"`
	Message string `json:"message,omitempty"`
}

// CompletedEvent represents an update completion event
type CompletedEvent struct {
	Success bool   `json:"success"`
	Reboot  bool   `json:"reboot,omitempty"`
	Message string `json:"message"`
	Duration string `json:"duration,omitempty"`
}

// FailedEvent represents an update failure event
type FailedEvent struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Code    int    `json:"code,omitempty"`
}
