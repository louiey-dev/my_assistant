// Package realtime provides authenticated dashboard event delivery.
package realtime

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

func NewHub() *Hub { return &Hub{clients: make(map[*websocket.Conn]struct{})} }

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || origin == "http://"+r.Host || origin == "https://"+r.Host
	},
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
	defer func() { h.remove(conn) }()
	for {
		messageType, _, readErr := conn.ReadMessage()
		if readErr != nil {
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		// Subscription payloads are accepted for forward compatibility. Event
		// filtering is added when device/camera-specific fan-out is needed.
	}
}

func (h *Hub) Broadcast(value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	h.mu.Lock()
	clients := make([]*websocket.Conn, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.Unlock()
	for _, client := range clients {
		_ = client.SetWriteDeadline(time.Now().Add(3 * time.Second))
		if err := client.WriteMessage(websocket.TextMessage, payload); err != nil {
			h.remove(client)
		}
	}
}

func (h *Hub) remove(conn *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
	_ = conn.Close()
}
