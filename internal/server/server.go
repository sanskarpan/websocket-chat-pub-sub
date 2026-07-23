package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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

	ipConnections sync.Map
	clientWG      sync.WaitGroup
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
	if s.cfg.Server.TLS.Enabled {
		httpServer.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	s.httpServer = httpServer

	go s.hub.Run()

	if s.pubsub != nil {
		s.wg.Add(2)
		go s.subscribeToRoomMessages()
		go s.subscribeToPresence()
	}

	if s.cfg.Server.TLS.Enabled && s.cfg.Server.TLS.CertFile != "" && s.cfg.Server.TLS.KeyFile != "" {
		return httpServer.ListenAndServeTLS(s.cfg.Server.TLS.CertFile, s.cfg.Server.TLS.KeyFile)
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
					select {
					case s.hub.broadcast <- &BroadcastMessage{
						RoomID: roomID,
						Data:   data,
					}:
					default:
						s.logger.Warn().Str("room_id", roomID).Msg("broadcast channel full, dropping message")
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

			if data.Presence == nil {
				continue
			}

			presenceMsg, err := protocol.NewServerMessage(protocol.ServerMsgPresence, map[string]interface{}{
				"user_id":  data.UserID,
				"status":   data.Presence.Status,
				"presence": data.Presence,
			})
			if err != nil {
				s.logger.Error().Err(err).Msg("Failed to create presence message")
				continue
			}
			presenceBytes, err := presenceMsg.ToJSON()
			if err != nil {
				s.logger.Error().Err(err).Msg("Failed to marshal presence message")
				continue
			}

			s.hub.mu.RLock()
			clients := make([]*Client, 0, len(s.hub.clients))
			for _, client := range s.hub.clients {
				if !client.isClosed() {
					clients = append(clients, client)
				}
			}
			s.hub.mu.RUnlock()

			for _, client := range clients {
				select {
				case client.send <- presenceBytes:
				default:
				}
			}
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
		s.logger.Warn().Msg("Timeout waiting for pub/sub goroutines to finish")
	}

	s.hub.CloseAll()

	clientDone := make(chan struct{})
	go func() {
		s.clientWG.Wait()
		close(clientDone)
	}()

	select {
	case <-clientDone:
	case <-time.After(10 * time.Second):
		s.logger.Warn().Msg("Timeout waiting for client goroutines to finish")
	}

	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		s.logger.Error().Err(err).Msg("Failed to encode health response")
	}
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var ipCounter *atomic.Int32
	if s.cfg.Server.Websocket.MaxConnectionsPerIP > 0 {
		ip := s.getClientIP(r)
		count, _ := s.ipConnections.LoadOrStore(ip, &atomic.Int32{})
		counter := count.(*atomic.Int32)
		if counter.Add(1) > int32(s.cfg.Server.Websocket.MaxConnectionsPerIP) {
			counter.Add(-1)
			http.Error(w, "Too many connections from this IP", http.StatusTooManyRequests)
			return
		}
		ipCounter = counter
	}

	defer func() {
		if r := recover(); r != nil {
			s.logger.Error().Interface("panic", r).Msg("panic in handleWebSocket")
			if ipCounter != nil {
				ipCounter.Add(-1)
			}
		}
	}()

	token := r.URL.Query().Get("token")
	if token == "" {
		token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}

	if token == "" {
		if ipCounter != nil {
			ipCounter.Add(-1)
		}
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	user, err := s.authService.ValidateToken(ctx, token)
	if err != nil {
		if ipCounter != nil {
			ipCounter.Add(-1)
		}
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	if s.pubsub != nil {
		for _, rule := range s.cfg.RateLimit.Rules {
			if rule.Key == "connection" && rule.Limit > 0 {
				allowed, err := s.pubsub.CheckRateLimit(ctx, "connection:"+user.ID, rule.Limit, rule.Window)
				if err == nil && !allowed {
					if ipCounter != nil {
						ipCounter.Add(-1)
					}
					http.Error(w, "Too many connections", http.StatusTooManyRequests)
					return
				}
			}
		}
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
		if ipCounter != nil {
			ipCounter.Add(-1)
		}
		return
	}

	client := NewClient(s.hub, conn, user.ID, s.logger, s.roomService, s.messageService, s.presenceService, s.cfg)
	client.ipCounter = ipCounter
	s.hub.register <- client

	s.clientWG.Add(2)
	go func() {
		defer s.clientWG.Done()
		client.ReadPump()
	}()
	go func() {
		defer s.clientWG.Done()
		client.WritePump()
	}()
}

func (s *Server) getAllowedOrigins() []string {
	return []string{"http://localhost:3000", "http://localhost:8085", "http://127.0.0.1:3000", "http://127.0.0.1:8085"}
}

func (s *Server) getClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type Hub struct {
	clients    map[string]*Client
	users      map[string]map[string]bool
	rooms      map[string]map[string]bool

	register   chan *Client
	unregister chan *Client
	broadcast  chan *BroadcastMessage

	pubsub pubsub.PubSub
	logger *zerolog.Logger
	mu     sync.RWMutex
	closed bool
	quit   chan struct{}
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
		quit:       make(chan struct{}),
	}
}

func (h *Hub) Run() {
	defer func() {
		for client := range h.unregister {
			h.cleanupClient(client)
		}
	}()

	for {
		select {
		case <-h.quit:
			return
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
			h.cleanupClient(client)

		case msg := <-h.broadcast:
			h.broadcastMessage(msg)
		}
	}
}

func (h *Hub) cleanupClient(client *Client) {
	h.mu.Lock()
	subsRemoved := 0
	alreadyClosed := client.closed.Load()
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
				subsRemoved++
			}
		}
		if !alreadyClosed {
			client.closed.Store(true)
		}
	}
	h.mu.Unlock()

	if !alreadyClosed {
		metrics.WebsocketConnectionsActive.Dec()
		for i := 0; i < subsRemoved; i++ {
			metrics.RoomSubscriptionsActive.Dec()
		}
	}

	h.logger.Info().Str("user_id", client.userID).Str("conn_id", client.id).Msg("Client disconnected")
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

		if client.isClosed() {
			continue
		}

		select {
		case client.send <- msg.Data:
		default:
			h.mu.Lock()
			if c, exists := h.clients[connID]; exists && c == client {
				if !client.closed.Swap(true) {
					if client.cancel != nil {
						client.cancel()
					}
				}
				delete(h.clients, connID)
				delete(h.users[client.userID], connID)
				if len(h.users[client.userID]) == 0 {
					delete(h.users, client.userID)
				}
				subsRemoved := 0
				for roomID := range client.rooms {
					if h.rooms[roomID] != nil {
						delete(h.rooms[roomID], connID)
						if len(h.rooms[roomID]) == 0 {
							delete(h.rooms, roomID)
						}
						subsRemoved++
					}
				}
				h.mu.Unlock()

				metrics.WebsocketConnectionsActive.Dec()
				for i := 0; i < subsRemoved; i++ {
					metrics.RoomSubscriptionsActive.Dec()
				}
			} else {
				h.mu.Unlock()
			}
		}
	}
}

func (h *Hub) SubscribeToRoom(client *Client, roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if client.isClosed() {
		return
	}

	if h.rooms[roomID] == nil {
		h.rooms[roomID] = make(map[string]bool)
	}
	if !h.rooms[roomID][client.id] {
		h.rooms[roomID][client.id] = true
		metrics.RoomSubscriptionsActive.Inc()
	}
	client.rooms[roomID] = true
}

func (h *Hub) UnsubscribeFromRoom(client *Client, roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.rooms[roomID] != nil {
		if h.rooms[roomID][client.id] {
			delete(h.rooms[roomID], client.id)
			metrics.RoomSubscriptionsActive.Dec()
		}
		if len(h.rooms[roomID]) == 0 {
			delete(h.rooms, roomID)
		}
	}
	delete(client.rooms, roomID)
}

func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return
	}
	h.closed = true
	close(h.quit)

	for _, client := range h.clients {
		if !client.closed.Swap(true) {
			if client.cancel != nil {
				client.cancel()
			}
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
	closed          atomic.Bool
	closedInProgress atomic.Bool
	ctx             context.Context
	cancel          context.CancelFunc
	ipCounter       *atomic.Int32
}

func NewClient(hub *Hub, conn *websocket.Conn, userID string, logger *zerolog.Logger, roomService *service.RoomService, messageService *service.MessageService, presenceService *service.PresenceService, cfg *config.Config) *Client {
	ctx, cancel := context.WithCancel(context.Background())
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
		ctx:             ctx,
		cancel:          cancel,
	}
}

func (c *Client) ReadPump() {
	defer func() {
		c.cancel()
		select {
		case c.hub.unregister <- c:
		default:
		}
		c.conn.Close()
		if c.ipCounter != nil {
			c.ipCounter.Add(-1)
		}
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
				metrics.ConnectionErrorsTotal.WithLabelValues("read_error").Inc()
			}
			break
		}

		c.handleMessage(message)
		metrics.MessagesReceivedTotal.WithLabelValues("chat").Inc()
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
		case <-c.ctx.Done():
			c.drainAndClose()
			return
		case message, ok := <-c.send:
			if !ok {
				return
			}

			c.conn.SetWriteDeadline(time.Now().Add(c.cfg.Server.Websocket.WriteTimeout))
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

func (c *Client) drainAndClose() {
	if c.closedInProgress.Swap(true) {
		return
	}
	close(c.send)
}

func (c *Client) isClosed() bool {
	return c.closed.Load() || c.closedInProgress.Load()
}

func (c *Client) handleMessage(data []byte) {
	var msg protocol.ClientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		c.logger.Error().Err(err).Msg("Failed to parse message")
		return
	}

	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	switch msg.Type {
	case protocol.ClientMsgSubscribe:
		c.handleSubscribe(ctx, msg)
	case protocol.ClientMsgUnsubscribe:
		c.handleUnsubscribe(msg)
	case protocol.ClientMsgMessage:
		c.handleChatMessage(ctx, msg)
	case protocol.ClientMsgTyping:
		c.handleTyping(ctx, msg)
	case protocol.ClientMsgPing:
		c.handlePing(msg)
	case protocol.ClientMsgPresence:
		c.handlePresence(ctx, msg)
	case protocol.ClientMsgReaction:
		c.handleReaction(ctx, msg)
	case protocol.ClientMsgEdit:
		c.handleEdit(ctx, msg)
	case protocol.ClientMsgDelete:
		c.handleDelete(ctx, msg)
	case protocol.ClientMsgReadReceipt:
		c.handleReadReceipt(ctx, msg)
	default:
		c.logger.Warn().Str("type", string(msg.Type)).Msg("unknown message type")
		c.sendError(msg.ID, "UNKNOWN_TYPE", "unknown message type: "+string(msg.Type))
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

	ack, err := protocol.NewServerMessage(protocol.ServerMsgAck, protocol.AckData{
		ClientMsgID: msg.ID,
		Status:      "subscribed",
	})
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to create ack message")
		return
	}
	ackBytes, _ := ack.ToJSON()
	select {
	case c.send <- ackBytes:
	default:
	}
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

	if data.RoomID == "" {
		c.sendError(msg.ID, "INVALID_INPUT", "room_id is required")
		return
	}

	if data.Content == "" {
		c.sendError(msg.ID, "INVALID_INPUT", "content cannot be empty")
		return
	}

	if len(data.Content) > 4000 {
		c.sendError(msg.ID, "CONTENT_TOO_LONG", "content exceeds 4000 characters")
		return
	}

	isMember, err := c.roomService.IsMember(ctx, data.RoomID, c.userID)
	if err != nil || !isMember {
		c.sendError(msg.ID, "FORBIDDEN", "not a member of this room")
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
		c.sendError(msg.ID, "SEND_FAILED", err.Error())
		return
	}

	ack, err := protocol.NewServerMessage(protocol.ServerMsgAck, protocol.AckData{
		ClientMsgID: msg.ID,
		ServerMsgID: savedMsg.ID,
		Status:      "delivered",
	})
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to create ack")
		return
	}
	ackBytes, _ := ack.ToJSON()
	select {
	case c.send <- ackBytes:
	default:
	}

	metrics.MessagesSentTotal.WithLabelValues("chat").Inc()
}

func (c *Client) sendError(clientMsgID, code, message string) {
	errorMsg, err := protocol.NewServerMessage(protocol.ServerMsgError, protocol.ErrorData{
		ClientMsgID: clientMsgID,
		Code:        code,
		Message:     message,
	})
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to create error message")
		return
	}
	errorBytes, _ := errorMsg.ToJSON()
	select {
	case c.send <- errorBytes:
	default:
	}
}

func (c *Client) handleTyping(ctx context.Context, msg protocol.ClientMessage) {
	var data protocol.TypingData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return
	}

	if data.RoomID == "" {
		return
	}

	isMember, _ := c.roomService.IsMember(ctx, data.RoomID, c.userID)
	if !isMember {
		return
	}

	data.UserID = c.userID
	typingMsg, err := protocol.NewServerMessage(protocol.ServerMsgTyping, data)
	if err != nil {
		c.logger.Error().Err(err).Msg("Failed to create typing message")
		return
	}
	typingBytes, _ := typingMsg.ToJSON()

	select {
	case c.hub.broadcast <- &BroadcastMessage{
		RoomID: data.RoomID,
		Data:   typingBytes,
	}:
	default:
	}
}

func (c *Client) handlePing(msg protocol.ClientMessage) {
	pong, err := protocol.NewServerMessage(protocol.ServerMsgPong, nil)
	if err != nil {
		return
	}
	pongBytes, _ := pong.ToJSON()
	select {
	case c.send <- pongBytes:
	default:
	}
}

func (c *Client) handlePresence(ctx context.Context, msg protocol.ClientMessage) {
	var data protocol.PresenceData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return
	}

	validStatuses := map[string]bool{
		"online":  true,
		"away":    true,
		"dnd":     true,
		"offline": true,
	}
	if !validStatuses[data.Status] {
		c.sendError(msg.ID, "INVALID_INPUT", "invalid status value")
		return
	}

	if c.presenceService != nil {
		if err := c.presenceService.SetPresence(ctx, c.userID, model.UserStatus(data.Status), model.ClientInfo{}); err != nil {
			c.logger.Error().Err(err).Msg("Failed to set presence")
		}
	}
}

func (c *Client) handleReaction(ctx context.Context, msg protocol.ClientMessage) {
	var data protocol.ReactionData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.sendError(msg.ID, "INVALID_INPUT", "invalid reaction payload")
		return
	}

	if data.MessageID == "" || data.Emoji == "" {
		c.sendError(msg.ID, "INVALID_INPUT", "message_id and emoji required")
		return
	}

	var err error
	if data.Action == "remove" {
		err = c.messageService.RemoveReaction(ctx, data.MessageID, data.Emoji, c.userID)
	} else {
		err = c.messageService.AddReaction(ctx, data.MessageID, data.Emoji, c.userID)
	}
	if err != nil {
		c.sendError(msg.ID, "REACTION_FAILED", err.Error())
		return
	}

	ack, _ := protocol.NewServerMessage(protocol.ServerMsgAck, protocol.AckData{
		ClientMsgID: msg.ID,
		Status:      "reaction_" + data.Action,
	})
	ackBytes, _ := ack.ToJSON()
	select {
	case c.send <- ackBytes:
	default:
	}
}

func (c *Client) handleEdit(ctx context.Context, msg protocol.ClientMessage) {
	var data protocol.EditData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.sendError(msg.ID, "INVALID_INPUT", "invalid edit payload")
		return
	}

	if data.MessageID == "" {
		c.sendError(msg.ID, "INVALID_INPUT", "message_id required")
		return
	}

	if err := c.messageService.EditMessage(ctx, data.MessageID, c.userID, data.Content); err != nil {
		c.sendError(msg.ID, "EDIT_FAILED", err.Error())
		return
	}

	ack, _ := protocol.NewServerMessage(protocol.ServerMsgAck, protocol.AckData{
		ClientMsgID: msg.ID,
		Status:      "edited",
	})
	ackBytes, _ := ack.ToJSON()
	select {
	case c.send <- ackBytes:
	default:
	}
}

func (c *Client) handleDelete(ctx context.Context, msg protocol.ClientMessage) {
	var data protocol.DeleteData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		c.sendError(msg.ID, "INVALID_INPUT", "invalid delete payload")
		return
	}

	if data.MessageID == "" {
		c.sendError(msg.ID, "INVALID_INPUT", "message_id required")
		return
	}

	if err := c.messageService.DeleteMessage(ctx, data.MessageID, c.userID); err != nil {
		c.sendError(msg.ID, "DELETE_FAILED", err.Error())
		return
	}

	ack, _ := protocol.NewServerMessage(protocol.ServerMsgAck, protocol.AckData{
		ClientMsgID: msg.ID,
		Status:      "deleted",
	})
	ackBytes, _ := ack.ToJSON()
	select {
	case c.send <- ackBytes:
	default:
	}
}

func (c *Client) handleReadReceipt(ctx context.Context, msg protocol.ClientMessage) {
	var data protocol.ReadReceiptData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return
	}

	if data.MessageID == "" || data.RoomID == "" {
		return
	}

	if c.roomService != nil {
		if err := c.roomService.MarkRead(ctx, data.RoomID, c.userID, data.MessageID); err != nil {
			c.logger.Error().Err(err).Msg("Failed to mark message as read")
		}
	}
}