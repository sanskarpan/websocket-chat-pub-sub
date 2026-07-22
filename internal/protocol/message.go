package protocol

import (
	"encoding/json"
	"time"
)

type ClientMessageType string

const (
	ClientMsgSubscribe   ClientMessageType = "subscribe"
	ClientMsgUnsubscribe ClientMessageType = "unsubscribe"
	ClientMsgMessage     ClientMessageType = "message"
	ClientMsgTyping      ClientMessageType = "typing"
	ClientMsgReadReceipt ClientMessageType = "read_receipt"
	ClientMsgReaction    ClientMessageType = "reaction"
	ClientMsgEdit        ClientMessageType = "edit"
	ClientMsgDelete      ClientMessageType = "delete"
	ClientMsgPresence    ClientMessageType = "presence"
	ClientMsgPing        ClientMessageType = "ping"
)

type ServerMessageType string

const (
	ServerMsgConnection     ServerMessageType = "connection"
	ServerMsgAck            ServerMessageType = "ack"
	ServerMsgError          ServerMessageType = "error"
	ServerMsgNewMessage     ServerMessageType = "new_message"
	ServerMsgMessageUpdated ServerMessageType = "message_updated"
	ServerMsgTyping         ServerMessageType = "typing"
	ServerMsgPresence       ServerMessageType = "presence"
	ServerMsgReadReceipt    ServerMessageType = "read_receipt"
	ServerMsgReaction       ServerMessageType = "reaction"
	ServerMsgRoomUpdated    ServerMessageType = "room_updated"
	ServerMsgMemberJoined   ServerMessageType = "member_joined"
	ServerMsgMemberLeft     ServerMessageType = "member_left"
	ServerMsgSystem         ServerMessageType = "system"
	ServerMsgPong           ServerMessageType = "pong"
)

type ClientMessage struct {
	ID        string            `json:"id"`
	Type      ClientMessageType `json:"type"`
	Timestamp time.Time         `json:"timestamp"`
	Data      json.RawMessage   `json:"data"`
}

type ServerMessage struct {
	ID        string            `json:"id"`
	Type      ServerMessageType `json:"type"`
	Timestamp time.Time         `json:"timestamp"`
	Data      json.RawMessage   `json:"data"`
}

type SubscribeData struct {
	RoomIDs           []string `json:"room_ids"`
	PresenceSubscribe []string `json:"presence_subscribe"`
}

type MessageData struct {
	RoomID   string  `json:"room_id"`
	Content  string  `json:"content"`
	ClientID string  `json:"client_id,omitempty"`
	ParentID *string `json:"parent_id,omitempty"`
}

type TypingData struct {
	RoomID string `json:"room_id"`
	UserID string `json:"user_id"`
}

type ReadReceiptData struct {
	RoomID    string `json:"room_id"`
	MessageID string `json:"message_id"`
	UserID    string `json:"user_id"`
}

type ReactionData struct {
	RoomID    string `json:"room_id"`
	MessageID string `json:"message_id"`
	Emoji     string `json:"emoji"`
	Action    string `json:"action"`
	UserID    string `json:"user_id"`
}

type EditData struct {
	RoomID    string `json:"room_id"`
	MessageID string `json:"message_id"`
	Content   string `json:"content"`
}

type DeleteData struct {
	RoomID    string `json:"room_id"`
	MessageID string `json:"message_id"`
}

type PresenceData struct {
	Status string `json:"status"`
}

type AckData struct {
	ClientMsgID string `json:"client_msg_id"`
	ServerMsgID string `json:"server_msg_id,omitempty"`
	Status      string `json:"status"`
}

type ErrorData struct {
	ClientMsgID string `json:"client_msg_id,omitempty"`
	Code        string `json:"code"`
	Message     string `json:"message"`
	RetryAfter  int    `json:"retry_after,omitempty"`
}

type ConnectionData struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
}

type RoomSubscriptionData struct {
	RoomID        string      `json:"room_id"`
	LastMessage   interface{} `json:"last_message,omitempty"`
	UnreadCount   int         `json:"unread_count"`
	MembersOnline []string    `json:"members_online"`
}

type NewMessageData struct {
	RoomID  string      `json:"room_id"`
	Message interface{} `json:"message"`
}

func NewServerMessage(msgType ServerMessageType, data interface{}) (*ServerMessage, error) {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	return &ServerMessage{
		ID:        generateID(),
		Type:      msgType,
		Timestamp: time.Now(),
		Data:      dataBytes,
	}, nil
}

func (m *ServerMessage) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

func generateID() string {
	return time.Now().Format("20060102150405.000000")
}
