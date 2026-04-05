package ws

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Event — событие, отправляемое клиенту через WebSocket
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Client — одно WebSocket соединение
type Client struct {
	hub       *Hub
	userID    string
	conn      *websocket.Conn
	send      chan []byte
	closeOnce sync.Once
}

// Hub — менеджер всех активных WebSocket соединений
type Hub struct {
	mu      sync.RWMutex
	clients map[string][]*Client
}

// NewHub создаёт новый Hub
func NewHub() *Hub {
	return &Hub{clients: make(map[string][]*Client)}
}

// Register — добавляет соединение в Hub и запускает writePump
func (h *Hub) Register(userID string, conn *websocket.Conn) *Client {
	c := &Client{
		hub:    h,
		userID: userID,
		conn:   conn,
		send:   make(chan []byte, 256),
	}
	h.mu.Lock()
	h.clients[userID] = append(h.clients[userID], c)
	h.mu.Unlock()
	go c.writePump()
	return c
}

// Unregister — удаляет соединение из Hub (идемпотентно через sync.Once)
func (h *Hub) Unregister(c *Client) {
	c.closeOnce.Do(func() {
		h.mu.Lock()
		list := h.clients[c.userID]
		for i, cc := range list {
			if cc == c {
				h.clients[c.userID] = append(list[:i], list[i+1:]...)
				close(c.send)
				break
			}
		}
		if len(h.clients[c.userID]) == 0 {
			delete(h.clients, c.userID)
		}
		h.mu.Unlock()
	})
}

// Send — отправляет событие всем соединениям пользователя
func (h *Hub) Send(userID string, event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.RLock()
	clients := make([]*Client, len(h.clients[userID]))
	copy(clients, h.clients[userID])
	h.mu.RUnlock()

	for _, c := range clients {
		select {
		case c.send <- data:
		default:
			// Буфер переполнен — отключаем клиента
			go h.Unregister(c)
		}
	}
}

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

// writePump — отправляет сообщения клиенту и поддерживает keepalive ping
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.Printf("ws write error: %v", err)
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ReadPump — читает входящие сообщения (keepalive pong), блокирует до закрытия соединения
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()
	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("ws unexpected close: %v", err)
			}
			break
		}
	}
}
