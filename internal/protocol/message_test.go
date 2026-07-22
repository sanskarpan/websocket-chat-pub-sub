package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestClientMessage_JSONSerialization(t *testing.T) {
	msg := ClientMessage{
		ID:        "test-123",
		Type:      ClientMsgMessage,
		Timestamp: time.Now(),
		Data:      json.RawMessage(`{"room_id": "room-1", "content": "hello"}`),
	}

	data, err := json.Marshal(msg)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"type":"message"`)
}

func TestServerMessage_ToJSON(t *testing.T) {
	msg := ServerMessage{
		ID:        "srv-123",
		Type:      ServerMsgConnection,
		Timestamp: time.Now(),
		Data:      json.RawMessage(`{"user_id": "user-1", "session_id": "sess-1"}`),
	}

	data, err := msg.ToJSON()
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"type":"connection"`)
}

func TestNewServerMessage(t *testing.T) {
	data := map[string]string{"test": "value"}
	msg, err := NewServerMessage(ServerMsgAck, data)

	assert.NoError(t, err)
	assert.NotEmpty(t, msg.ID)
	assert.Equal(t, ServerMsgAck, msg.Type)
	assert.NotZero(t, msg.Timestamp)
}
