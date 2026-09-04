// Package realtime provides authenticated dashboard event delivery.
package realtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
	logger  *slog.Logger
}

type client struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func NewHub(loggers ...*slog.Logger) *Hub {
	var logger *slog.Logger
	if len(loggers) > 0 {
		logger = loggers[0]
	}
	return &Hub{clients: make(map[*client]struct{}), logger: logger}
}

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
	client := &client{conn: conn}
	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()
	if h.logger != nil {
		h.logger.Info("live connection opened", "event", "websocket_connected")
	}
	defer h.remove(client, nil)

	// A heartbeat makes half-open connections observable. Browsers reply to
	// WebSocket ping frames automatically; missing a reply closes the socket so
	// the frontend can reconnect instead of remaining permanently stale.
	conn.SetReadLimit(4096)
	_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(75 * time.Second))
	})
	stopPing := make(chan struct{})
	defer close(stopPing)
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopPing:
				return
			case <-ticker.C:
				if err := client.write(websocket.PingMessage, nil); err != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()
	for {
		messageType, _, readErr := conn.ReadMessage()
		if readErr != nil {
			h.remove(client, readErr)
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
	clients := make([]*client, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.Unlock()
	for _, client := range clients {
		if err := client.write(websocket.TextMessage, payload); err != nil {
			h.remove(client, err)
		}
	}
}

func (c *client) write(messageType int, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	return c.conn.WriteMessage(messageType, payload)
}

func (h *Hub) remove(client *client, reason error) {
	h.mu.Lock()
	_, connected := h.clients[client]
	delete(h.clients, client)
	h.mu.Unlock()
	if !connected {
		return
	}
	if h.logger != nil {
		attributes := []any{"event", "websocket_disconnected"}
		if reason != nil {
			attributes = append(attributes, "error_type", errorType(reason))
		}
		h.logger.Info("live connection closed", attributes...)
	}
	_ = client.conn.Close()
}

func errorType(err error) string {
	if err == nil {
		return "normal"
	}
	var closeError *websocket.CloseError
	if errors.As(err, &closeError) {
		return "websocket_close_" + fmt.Sprint(closeError.Code)
	}
	return fmt.Sprintf("%T", err)
}
