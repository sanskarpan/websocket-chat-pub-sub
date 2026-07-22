package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"github.com/websocket-chat/internal/config"
	"github.com/websocket-chat/internal/metrics"
	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/internal/protocol"
	"github.com/websocket-chat/internal/pubsub"
	"github.com/websocket-chat/internal/service"
)

type Server struct {
	cfg             *config.Config
	logger          *zerolog.Logger
	authService     *service.AuthService
	userService     *service.UserService
	roomService     *service.RoomService
	messageService  *service.MessageService
	presenceService *service.PresenceService
	pubsub          pubsub.PubSub

	hub        *Hub
	httpServer *http.Server
	wsPort     int
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func NewServer(
	cfg *config.Config,
	logger *zerolog.Logger,
	authService *service.AuthService,
	userService *service.UserService,
	roomService *service.RoomService,
	messageService *service.MessageService,
	presenceService *service.PresenceService,
	pubsub pubsub.PubSub,
) *Server {
	wsPort := cfg.Server.Port + 1
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		cfg:             cfg,
		logger:          logger,
		authService:     authService,
		userService:     userService,
		roomService:     roomService,
		messageService:  messageService,
		presenceService: presenceService,
		pubsub:          pubsub,
		hub:             NewHub(pubsub, logger),
		wsPort:          wsPort,
		ctx:             ctx,
		cancel:          cancel,
	}
}

func (s *Server) Start() error {
	s.logger.Info().Msg("Starting WebSocket server")

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/readyz", s.handleHealth)
	mux.HandleFunc("/ws", s.handleWebSocket)

	httpServer := &http.Server{
		Addr:         s.cfg.Server.Host + ":" + strconv.Itoa(s.wsPort),
		Handler:      mux,
		ReadTimeout:  s.cfg.Server.HTTP.ReadTimeout,
		WriteTimeout: s.cfg.Server.HTTP.WriteTimeout,
		IdleTimeout:  s.cfg.Server.HTTP.IdleTimeout,
	}

	s.httpServer = httpServer

	go s.hub.Run()

	if s.pubsub != nil {
		s.wg.Add(2)
		go s.subscribeToRoomMessages()
		go s.subscribeToPresence()
	}

	return httpServer.ListenAndServe()
}

func (s *Server) subscribeToRoomMessages() {
	defer s.wg.Done()

	backoff := 100 * time.Millisecond
	maxBackoff := 10 * time.Second

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		sub, err := s.pubsub.PSubscribe(s.ctx, "ws:room:*")
		if err != nil {
			s.logger.Error().Err(err).Msg("Failed to subscribe to room messages, retrying...")
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(backoff):
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}
		}

		backoff = 100 * time.Millisecond
		ch := sub.Channel()

	messageLoop:
		for {
			select {
			case <-s.ctx.Done():
				sub.Close()
				return
			case msg, ok := <-ch:
				if !ok {
					break messageLoop
				}

				roomID, eventType := extractRoomIDAndEvent(msg.Channel)
				if roomID == "" {
					continue
				}

				var serverMsg *protocol.ServerMessage

				if eventType == "events" {
					var eventData struct {
						UserID    string `json:"user_id"`
						Action    string `json:"action"`
						Timestamp int64  `json:"timestamp"`
					}
					if err := json.Unmarshal([]byte(msg.Payload), &eventData); err == nil {
						if eventData.Action == "joined" {
							serverMsg, _ = protocol.NewServerMessage(protocol.ServerMsgMemberJoined, map[string]interface{}{
								"room_id": roomID,
								"user_id": eventData.UserID,
							})
						} else if eventData.Action == "left" {
							serverMsg, _ = protocol.NewServerMessage(protocol.ServerMsgMemberLeft, map[string]interface{}{
								"room_id": roomID,
								"user_id": eventData.UserID,
							})
						}
					}
				} else {
					var msgData struct {
						RoomID  string      `json:"room_id"`
						Message interface{} `json:"message"`
						Action  string      `json:"action,omitempty"`
					}
					if err := json.Unmarshal([]byte(msg.Payload), &msgData); err == nil {
						switch msgData.Action {
						case "edited":
							serverMsg, _ = protocol.NewServerMessage(protocol.ServerMsgMessageUpdated, msgData)
						case "deleted":
							serverMsg, _ = protocol.NewServerMessage(protocol.ServerMsgMessageUpdated, msgData)
						default:
							serverMsg, _ = protocol.NewServerMessage(protocol.ServerMsgNewMessage, msgData)
						}
					}
				}

				if serverMsg != nil {
					data, _ := serverMsg.ToJSON()
					s.hub.broadcast <- &BroadcastMessage{
						RoomID: roomID,
						Data:   data,
					}
				}
			}
		}
		sub.Close()
	}
}

func extractRoomIDAndEvent(channel string) (roomID string, eventType string) {
	if !strings.HasPrefix(channel, "ws:room:") {
		return "", ""
	}

	rest := strings.TrimPrefix(channel, "ws:room:")

	if idx := strings.Index(rest, ":"); idx != -1 {
		return rest[:idx], rest[idx+1:]
	}

	return rest, ""
}

func (s *Server) subscribeToPresence() {
	defer s.wg.Done()

	backoff := 100 * time.Millisecond
	maxBackoff := 10 * time.Second

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		sub, err := s.pubsub.Subscribe(s.ctx, "ws:presence")
		if err != nil {
			s.logger.Error().Err(err).Msg("Failed to subscribe to presence, retrying...")
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(backoff):
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}
		}

		backoff = 100 * time.Millisecond
		ch := sub.Channel()

	presenceLoop:
		for {
			select {
			case <-s.ctx.Done():
				sub.Close()
				return
			case msg, ok := <-ch:
				if !ok {
					break presenceLoop
				}

				var data struct {
					UserID   string          `json:"user_id"`
					Presence *model.Presence `json:"presence"`
				}
				if err := json.Unmarshal([]byte(msg.Payload), &data); err != nil {
					continue
				}

				presenceMsg, _ := protocol.NewServerMessage(protocol.ServerMsgPresence, map[string]interface{}{
					"user_id":  data.UserID,
					"status":   data.Presence.Status,
					"presence": data.Presence,
				})
				presenceBytes, _ := presenceMsg.ToJSON()

				s.hub.mu.RLock()
				for _, client := range s.hub.clients {
					select {
					case client.send <- presenceBytes:
					default:
					}
				}
				s.hub.mu.RUnlock()
			}
		}
		sub.Close()
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("Shutting down server")

	s.cancel()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}

	s.hub.CloseAll()

	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token := r.URL.Query().Get("token")
	if token == "" {
		token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}

	if token == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := s.authService.ValidateToken(ctx, token)
	if err != nil {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	allowedOrigins := s.getAllowedOrigins()
	origin := r.Header.Get("Origin")

	upgrader := websocket.Upgrader{
		ReadBufferSize:  s.cfg.Server.Websocket.ReadBufferSize,
		WriteBufferSize: s.cfg.Server.Websocket.WriteBufferSize,
		CheckOrigin: func(r *http.Request) bool {
			if origin == "" {
				return true
			}
			for _, allowed := range allowedOrigins {
				if allowed == "*" || allowed == origin {
					return true
				}
			}
			return false
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to upgrade connection")
		return
	}

	client := NewClient(s.hub, conn, user.ID, s.logger, s.roomService, s.messageService, s.presenceService, s.cfg)
	s.hub.register <- client

	go client.ReadPump()
	go client.WritePump()
}

func (s *Server) getAllowedOrigins() []string {
	return []string{"http://localhost:3000", "http://localhost:8085", "http://127.0.0.1:3000", "http://127.0.0.1:8085"}
}

type Hub struct {
	clients map[string]*Client
	users   map[string]map[string]bool
	rooms   map[string]map[string]bool

	register   chan *Client
	unregister chan *Client
	broadcast  chan *BroadcastMessage

	pubsub pubsub.PubSub
	logger *zerolog.Logger
	mu     sync.RWMutex
}

func NewHub(ps pubsub.PubSub, logger *zerolog.Logger) *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		users:      make(map[string]map[string]bool),
		rooms:      make(map[string]map[string]bool),
		register:   make(chan *Client, 100),
		unregister: make(chan *Client, 100),
		broadcast:  make(chan *BroadcastMessage, 100),
		pubsub:     ps,
		logger:     logger,
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.id] = client
			if h.users[client.userID] == nil {
				h.users[client.userID] = make(map[string]bool)
			}
			h.users[client.userID][client.id] = true
			h.mu.Unlock()

			metrics.WebsocketConnectionsActive.Inc()

			h.logger.Info().Str("user_id", client.userID).Str("conn_id", client.id).Msg("Client connected")

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.id]; ok {
				delete(h.clients, client.id)
				delete(h.users[client.userID], client.id)
				if len(h.users[client.userID]) == 0 {
					delete(h.users, client.userID)
				}

				for roomID := range client.rooms {
					if h.rooms[roomID] != nil {
						delete(h.rooms[roomID], client.id)
						if len(h.rooms[roomID]) == 0 {
							delete(h.rooms, roomID)
						}
					}
				}
			}
			h.mu.Unlock()

			metrics.WebsocketConnectionsActive.Dec()

			h.logger.Info().Str("user_id", client.userID).Str("conn_id", client.id).Msg("Client disconnected")

		case msg := <-h.broadcast:
			h.broadcastMessage(msg)
		}
	}
}

func (h *Hub) broadcastMessage(msg *BroadcastMessage) {
	h.mu.RLock()
	clientIDs := make([]string, 0, len(h.rooms[msg.RoomID]))
	for connID := range h.rooms[msg.RoomID] {
		clientIDs = append(clientIDs, connID)
	}
	h.mu.RUnlock()

	for _, connID := range clientIDs {
		h.mu.RLock()
		client, ok := h.clients[connID]
		h.mu.RUnlock()

		if !ok {
			continue
		}

		select {
		case client.send <- msg.Data:
		default:
			h.mu.Lock()
			if c, exists := h.clients[connID]; exists && c == client {
				delete(h.clients, connID)
				delete(h.users[client.userID], connID)
				if len(h.users[client.userID]) == 0 {
					delete(h.users, client.userID)
				}
				for roomID := range client.rooms {
					if h.rooms[roomID] != nil {
						delete(h.rooms[roomID], connID)
						if len(h.rooms[roomID]) == 0 {
							delete(h.rooms, roomID)
						}
					}
				}
				close(client.send)
			}
			h.mu.Unlock()
		}
	}
}

func (h *Hub) SubscribeToRoom(client *Client, roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rooms[roomID] == nil {
		h.rooms[roomID] = make(map[string]bool)
	}
	h.rooms[roomID][client.id] = true
	client.rooms[roomID] = true

	metrics.RoomSubscriptionsActive.Inc()
}

func (h *Hub) UnsubscribeFromRoom(client *Client, roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rooms[roomID] != nil {
		delete(h.rooms[roomID], client.id)
		if len(h.rooms[roomID]) == 0 {
			delete(h.rooms, roomID)
		}
	}
	delete(client.rooms, roomID)

	metrics.RoomSubscriptionsActive.Dec()
}

func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, client := range h.clients {
		select {
		case <-client.send:
		default:
			close(client.send)
		}
	}
}

type BroadcastMessage struct {
	RoomID string
	Data   []byte
}

type Client struct {
	hub             *Hub
	conn            *websocket.Conn
	id              string
	userID          string
	rooms           map[string]bool
	send            chan []byte
	logger          *zerolog.Logger
	roomService     *service.RoomService
	messageService  *service.MessageService
	presenceService *service.PresenceService
	cfg             *config.Config
}

func NewClient(hub *Hub, conn *websocket.Conn, userID string, logger *zerolog.Logger, roomService *service.RoomService, messageService *service.MessageService, presenceService *service.PresenceService, cfg *config.Config) *Client {
	return &Client{
		hub:             hub,
		conn:            conn,
		id:              uuid.New().String(),
		userID:          userID,
		rooms:           make(map[string]bool),
		send:            make(chan []byte, 256),
		logger:          logger,
		roomService:     roomService,
		messageService:  messageService,
		presenceService: presenceService,
		cfg:             cfg,
	}
}

func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(c.cfg.Server.Websocket.MaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(c.cfg.Server.Websocket.PongTimeout))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(c.cfg.Server.Websocket.PongTimeout))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Error().Err(err).Msg("WebSocket error")
			}
			break
		}

		c.handleMessage(message)
	}
}

func (c *Client) WritePump() {
	ticker := time.NewTicker(c.cfg.Server.Websocket.PingInterval)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(c.cfg.Server.Websocket.WriteTimeout))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(c.cfg.Server.Websocket.WriteTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) handleMessage(data []byte) {
	var msg protocol.ClientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		c.logger.Error().Err(err).Msg("Failed to parse message")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch msg.Type {
	case protocol.ClientMsgSubscribe:
		c.handleSubscribe(ctx, msg)
	case protocol.ClientMsgUnsubscribe:
		c.handleUnsubscribe(msg)
	case protocol.ClientMsgMessage:
		c.handleChatMessage(ctx, msg)
	case protocol.ClientMsgTyping:
		c.handleTyping(msg)
	case protocol.ClientMsgPing:
		c.handlePing(msg)
	case protocol.ClientMsgPresence:
		c.handlePresence(ctx, msg)
	}
}

func (c *Client) handleSubscribe(ctx context.Context, msg protocol.ClientMessage) {
	var data protocol.SubscribeData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return
	}

	for _, roomID := range data.RoomIDs {
		isMember, _ := c.roomService.IsMember(ctx, roomID, c.userID)
		if isMember {
			c.hub.SubscribeToRoom(c, roomID)
		}
	}

	ack, _ := protocol.NewServerMessage(protocol.ServerMsgAck, protocol.AckData{
		ClientMsgID: msg.ID,
		Status:      "subscribed",
	})
	ackBytes, _ := ack.ToJSON()
	c.send <- ackBytes
}

func (c *Client) handleUnsubscribe(msg protocol.ClientMessage) {
	var data protocol.SubscribeData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return
	}

	for _, roomID := range data.RoomIDs {
		c.hub.UnsubscribeFromRoom(c, roomID)
	}
}

func (c *Client) handleChatMessage(ctx context.Context, msg protocol.ClientMessage) {
	var data protocol.MessageData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return
	}

	savedMsg, err := c.messageService.SendMessage(ctx, service.SendMessageInput{
		RoomID:      data.RoomID,
		UserID:      c.userID,
		Content:     data.Content,
		ContentType: model.ContentTypeText,
		ClientID:    data.ClientID,
		ParentID:    data.ParentID,
	})

	if err != nil {
		errorMsg, _ := protocol.NewServerMessage(protocol.ServerMsgError, protocol.ErrorData{
			ClientMsgID: msg.ID,
			Code:        "SEND_FAILED",
			Message:     err.Error(),
		})
		errorBytes, _ := errorMsg.ToJSON()
		c.send <- errorBytes
		return
	}

	ack, _ := protocol.NewServerMessage(protocol.ServerMsgAck, protocol.AckData{
		ClientMsgID: msg.ID,
		ServerMsgID: savedMsg.ID,
		Status:      "delivered",
	})
	ackBytes, _ := ack.ToJSON()
	c.send <- ackBytes

	metrics.MessagesSentTotal.WithLabelValues("chat").Inc()
}

func (c *Client) handleTyping(msg protocol.ClientMessage) {
	var data protocol.TypingData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return
	}

	data.UserID = c.userID
	typingMsg, _ := protocol.NewServerMessage(protocol.ServerMsgTyping, data)
	typingBytes, _ := typingMsg.ToJSON()

	c.hub.broadcast <- &BroadcastMessage{
		RoomID: data.RoomID,
		Data:   typingBytes,
	}
}

func (c *Client) handlePing(msg protocol.ClientMessage) {
	pong, _ := protocol.NewServerMessage(protocol.ServerMsgPong, nil)
	pongBytes, _ := pong.ToJSON()
	c.send <- pongBytes
}

func (c *Client) handlePresence(ctx context.Context, msg protocol.ClientMessage) {
	var data protocol.PresenceData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return
	}

	if c.presenceService != nil {
		c.presenceService.SetPresence(ctx, c.userID, model.UserStatus(data.Status), model.ClientInfo{})
	}
}
