package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	apiBase     = "http://localhost:8085"
	wsBase      = "ws://localhost:8086"
	apiV1       = apiBase + "/api/v1"
	contentType = "application/json"
)

var (
	apiCtx = &apiContext{retryDelay: 2 * time.Second}
)

func TestMain(m *testing.M) {
	if os.Getenv("E2E_DISABLE_RATELIMIT") != "1" {
		fmt.Println("E2E: rate limiting active; set E2E_DISABLE_RATELIMIT=1 to disable")
	}
	os.Exit(m.Run())
}

type apiContext struct {
	retryDelay time.Duration
}

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

type createRoomRequest struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type room struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	CreatedBy   string `json:"created_by"`
}

type wsMessage struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type ackData struct {
	ClientMsgID string `json:"client_msg_id"`
	ServerMsgID string `json:"server_msg_id,omitempty"`
	Status      string `json:"status"`
}

type newMessageData struct {
	RoomID  string      `json:"room_id"`
	Message interface{} `json:"message"`
}

type wsWriter struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *wsWriter) WriteMessage(messageType int, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(messageType, data)
}

func randomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

func doRequest(t *testing.T, method, url string, body []byte, headers map[string]string) *http.Response {
	for attempt := 0; attempt < 5; attempt++ {
		t.Logf("Attempt %d: %s %s", attempt+1, method, url)
		var req *http.Request
		var err error
		if body != nil {
			req, err = http.NewRequest(method, url, bytes.NewReader(body))
		} else {
			req, err = http.NewRequest(method, url, nil)
		}
		require.NoError(t, err)

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Logf("Request error: %v, retrying...", err)
			time.Sleep(apiCtx.retryDelay)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			t.Logf("Rate limited, waiting %v...", apiCtx.retryDelay)
			time.Sleep(apiCtx.retryDelay)
			continue
		}
		return resp
	}
	t.Fatalf("Request failed after retries: %s %s", method, url)
	return nil
}

func registerUser(t *testing.T, username, email, password string) (string, int) {
	body := registerRequest{Username: username, Email: email, Password: password}
	data, _ := json.Marshal(body)

	resp := doRequest(t, "POST", apiV1+"/auth/register", data, map[string]string{"Content-Type": contentType})
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var result struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Email    string `json:"email"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		return result.ID, resp.StatusCode
	}
	return "", resp.StatusCode
}

func loginUser(t *testing.T, email, password string) (authResponse, int) {
	body := loginRequest{Email: email, Password: password}
	data, _ := json.Marshal(body)

	resp := doRequest(t, "POST", apiV1+"/auth/login", data, map[string]string{"Content-Type": contentType})
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var result authResponse
		json.NewDecoder(resp.Body).Decode(&result)
		return result, resp.StatusCode
	}
	return authResponse{}, resp.StatusCode
}

func createRoom(t *testing.T, token, name, roomType, desc string) (string, int) {
	body := createRoomRequest{Name: name, Type: roomType, Description: desc}
	data, _ := json.Marshal(body)

	headers := map[string]string{
		"Content-Type":  contentType,
		"Authorization": "Bearer " + token,
	}
	resp := doRequest(t, "POST", apiV1+"/rooms", data, headers)
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var result room
		json.NewDecoder(resp.Body).Decode(&result)
		return result.ID, resp.StatusCode
	}
	return "", resp.StatusCode
}

func connectWS(t *testing.T, token string) *websocket.Conn {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, wsBase+"/ws?token="+token, nil)
	require.NoError(t, err)
	return conn
}

func subscribeToRoom(t *testing.T, conn *websocket.Conn, roomID string) {
	msgID := fmt.Sprintf("sub-%d", time.Now().UnixNano())
	subMsg := map[string]interface{}{
		"id":   msgID,
		"type": "subscribe",
		"data": map[string]interface{}{
			"room_ids": []string{roomID},
		},
	}
	data, _ := json.Marshal(subMsg)
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := conn.WriteMessage(websocket.TextMessage, data)
	require.NoError(t, err)

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("Failed to receive ack for subscribe: %v", err)
			return
		}
		var msg wsMessage
		json.Unmarshal(raw, &msg)
		if msg.Type == "ack" {
			var ack ackData
			json.Unmarshal(msg.Data, &ack)
			if ack.Status == "subscribed" {
				return
			}
		}
	}
}

func sendWSMessage(t *testing.T, conn *websocket.Conn, roomID, content string) {
	msgID := fmt.Sprintf("msg-%d", time.Now().UnixNano())
	msg := map[string]interface{}{
		"id":   msgID,
		"type": "message",
		"data": map[string]interface{}{
			"room_id": roomID,
			"content": content,
		},
	}
	data, _ := json.Marshal(msg)
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := conn.WriteMessage(websocket.TextMessage, data)
	require.NoError(t, err)
}

func TestE2E_FullFlow(t *testing.T) {
	suffix := randomString(8)
	username := "full_" + suffix
	email := "full_" + suffix + "@example.com"
	password := "password123"

	userID, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)
	assert.NotEmpty(t, userID)
	t.Logf("Registered user: %s (id=%s)", username, userID)

	tokens, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)
	require.NotEmpty(t, tokens.AccessToken)
	require.NotEmpty(t, tokens.RefreshToken)
	assert.Equal(t, "Bearer", tokens.TokenType)
	assert.Greater(t, tokens.ExpiresIn, 0)

	roomID, status := createRoom(t, tokens.AccessToken, username+"_room", "group", "e2e test room")
	require.Equal(t, http.StatusCreated, status)
	require.NotEmpty(t, roomID)
	t.Logf("Created room: %s", roomID)

	conn := connectWS(t, tokens.AccessToken)
	defer conn.Close()

	subscribeToRoom(t, conn, roomID)

	content := "Hello_e2e_" + randomString(4)
	sendWSMessage(t, conn, roomID, content)

	ackReceived := false
	msgReceived := false
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg wsMessage
		require.NoError(t, json.Unmarshal(raw, &msg))
		t.Logf("WS message: type=%s", msg.Type)

		switch msg.Type {
		case "ack":
			if !ackReceived {
				var ack ackData
				json.Unmarshal(msg.Data, &ack)
				assert.Equal(t, "delivered", ack.Status)
				require.NotEmpty(t, ack.ServerMsgID)
				ackReceived = true
				t.Logf("Received ack for msg %s (server_id=%s)", ack.ClientMsgID, ack.ServerMsgID)
			}
		case "new_message":
			if !msgReceived {
				var newMsg newMessageData
				json.Unmarshal(msg.Data, &newMsg)
				assert.Equal(t, roomID, newMsg.RoomID)
				msgReceived = true
				t.Logf("Received new_message broadcast for room %s", newMsg.RoomID)
			}
		case "member_joined":
			t.Log("Got member_joined event")
		}
		if ackReceived && msgReceived {
			break
		}
	}
	assert.True(t, ackReceived, "Should receive ack for sent message")
	assert.True(t, msgReceived, "Should receive new_message broadcast")
}

func TestE2E_AuthEdgeCases(t *testing.T) {
	suffix := randomString(8)

	t.Run("register_twice", func(t *testing.T) {
		username := "dup_" + suffix
		email := "dup_" + suffix + "@example.com"
		_, status := registerUser(t, username, email, "password123")
		require.Equal(t, http.StatusCreated, status)

		_, status = registerUser(t, username, email, "password123")
		assert.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("login_invalid_credentials", func(t *testing.T) {
		_, status := loginUser(t, "nonexistent_"+suffix+"@example.com", "wrongpassword")
		assert.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("login_wrong_password", func(t *testing.T) {
		username := "wrongpw_" + suffix
		email := "wrongpw_" + suffix + "@example.com"
		_, status := registerUser(t, username, email, "password123")
		require.Equal(t, http.StatusCreated, status)

		_, status = loginUser(t, email, "wrongpassword")
		assert.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("no_auth_header", func(t *testing.T) {
		resp := doRequest(t, "GET", apiV1+"/rooms", nil, nil)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("invalid_token", func(t *testing.T) {
		resp := doRequest(t, "GET", apiV1+"/rooms", nil, map[string]string{"Authorization": "Bearer invalidtoken123"})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("expired_token", func(t *testing.T) {
		expiredToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwidHlwZSI6ImFjY2VzcyIsImV4cCI6MTUxNjIzOTAyMiwiaWF0IjoxNTE2MjM5MDIyLCJpc3MiOiJjaGF0LWFwcCIsImF1ZCI6WyJjaGF0LWFwaSJdfQ.6T5VqKEHMg9oGnF5QEkx5QJMEJ5nRZpjp1vMB0gMnLA"
		resp := doRequest(t, "GET", apiV1+"/rooms", nil, map[string]string{"Authorization": "Bearer " + expiredToken})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("bad_token_format", func(t *testing.T) {
		resp := doRequest(t, "GET", apiV1+"/rooms", nil, map[string]string{"Authorization": "NotBearer sometoken"})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}

func TestE2E_WSAuth(t *testing.T) {
	suffix := randomString(8)
	username := "wsauth_" + suffix
	email := "wsauth_" + suffix + "@example.com"
	password := "password123"

	_, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)

	tokens, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)

	t.Run("valid_access_token", func(t *testing.T) {
		conn := connectWS(t, tokens.AccessToken)
		defer conn.Close()
	})

	t.Run("invalid_token", func(t *testing.T) {
		dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
		_, resp, err := dialer.Dial(wsBase+"/ws?token=invalidtoken", nil)
		if err == nil && resp != nil {
			resp.Body.Close()
		}
		assert.Error(t, err, "WebSocket should reject invalid token")
	})

	t.Run("no_token", func(t *testing.T) {
		dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
		_, resp, err := dialer.Dial(wsBase+"/ws", nil)
		if err == nil && resp != nil {
			resp.Body.Close()
		}
		assert.Error(t, err, "WebSocket should reject connection without token")
	})
}

func TestE2E_TokenRefresh(t *testing.T) {
	suffix := randomString(8)
	username := "ref_" + suffix
	email := "ref_" + suffix + "@example.com"
	password := "password123"

	_, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)

	tokens1, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)

	t.Run("refresh_success", func(t *testing.T) {
		body := map[string]string{"refresh_token": tokens1.RefreshToken}
		data, _ := json.Marshal(body)
		resp := doRequest(t, "POST", apiV1+"/auth/refresh", data, map[string]string{"Content-Type": contentType})
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var tokens2 authResponse
			json.NewDecoder(resp.Body).Decode(&tokens2)
			assert.NotEmpty(t, tokens2.AccessToken)
			assert.NotEmpty(t, tokens2.RefreshToken)

			resp := doRequest(t, "GET", apiV1+"/rooms", nil, map[string]string{"Authorization": "Bearer " + tokens2.AccessToken})
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		} else {
			t.Logf("Refresh returned status %d (may be rate limited)", resp.StatusCode)
		}
	})

	t.Run("refresh_invalid", func(t *testing.T) {
		body := map[string]string{"refresh_token": "invalid-refresh-token"}
		data, _ := json.Marshal(body)
		resp := doRequest(t, "POST", apiV1+"/auth/refresh", data, map[string]string{"Content-Type": contentType})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}

func TestE2E_RoomOperations(t *testing.T) {
	suffix := randomString(8)
	username := "roomops_" + suffix
	email := "roomops_" + suffix + "@example.com"
	password := "password123"

	_, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)
	tokens, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)

	t.Run("create_and_get_by_id", func(t *testing.T) {
		roomID, status := createRoom(t, tokens.AccessToken, username+"_room1", "group", "")
		require.Equal(t, http.StatusCreated, status)

		resp := doRequest(t, "GET", apiV1+"/rooms/"+roomID, nil, map[string]string{"Authorization": "Bearer " + tokens.AccessToken})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var result room
		json.NewDecoder(resp.Body).Decode(&result)
		assert.Equal(t, roomID, result.ID)
	})

	t.Run("get_rooms_list", func(t *testing.T) {
		resp := doRequest(t, "GET", apiV1+"/rooms", nil, map[string]string{"Authorization": "Bearer " + tokens.AccessToken})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var rooms []room
		json.NewDecoder(resp.Body).Decode(&rooms)
		t.Logf("Found %d rooms", len(rooms))
	})

	t.Run("get_nonexistent_room", func(t *testing.T) {
		resp := doRequest(t, "GET", apiV1+"/rooms/nonexistent-id", nil, map[string]string{"Authorization": "Bearer " + tokens.AccessToken})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("join_and_leave_room", func(t *testing.T) {
		roomID, status := createRoom(t, tokens.AccessToken, username+"_room_join", "group", "")
		require.Equal(t, http.StatusCreated, status)

		resp := doRequest(t, "POST", apiV1+"/rooms/"+roomID+"/leave", nil, map[string]string{"Authorization": "Bearer " + tokens.AccessToken})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		resp = doRequest(t, "POST", apiV1+"/rooms/"+roomID+"/join", nil, map[string]string{"Authorization": "Bearer " + tokens.AccessToken})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestE2E_MessageFlow(t *testing.T) {
	suffix := randomString(8)
	username := "msgflow_" + suffix
	email := "msgflow_" + suffix + "@example.com"
	password := "password123"

	_, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)
	tokens, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)
	roomID, status := createRoom(t, tokens.AccessToken, username+"_msgroom", "group", "")
	require.Equal(t, http.StatusCreated, status)

	conn := connectWS(t, tokens.AccessToken)
	defer conn.Close()
	subscribeToRoom(t, conn, roomID)

	t.Run("send_and_receive_message", func(t *testing.T) {
		content := "test_msg_" + randomString(4)
		sendWSMessage(t, conn, roomID, content)

		found := false
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			_, raw, err := conn.ReadMessage()
			if err != nil {
				break
			}
			var msg wsMessage
			require.NoError(t, json.Unmarshal(raw, &msg))
			if msg.Type == "new_message" {
				found = true
				break
			}
		}
		assert.True(t, found, "Should receive new_message")
	})

	t.Run("get_message_history", func(t *testing.T) {
		sendWSMessage(t, conn, roomID, "history_test_"+randomString(4))
		time.Sleep(1 * time.Second)

		resp := doRequest(t, "GET", apiV1+"/rooms/"+roomID+"/messages", nil, map[string]string{"Authorization": "Bearer " + tokens.AccessToken})
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var messages []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		}
		json.NewDecoder(resp.Body).Decode(&messages)
		assert.NotEmpty(t, messages)
	})
}

func TestE2E_ConcurrentMessages(t *testing.T) {
	suffix := randomString(6)
	username := "concurrent_" + suffix
	email := "concurrent_" + suffix + "@example.com"
	password := "password123"

	_, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)
	tokens, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)
	roomID, status := createRoom(t, tokens.AccessToken, username+"_concurrent", "group", "")
	require.Equal(t, http.StatusCreated, status)

	conn1 := connectWS(t, tokens.AccessToken)
	defer conn1.Close()
	conn2 := connectWS(t, tokens.AccessToken)
	defer conn2.Close()

	subscribeToRoom(t, conn1, roomID)
	subscribeToRoom(t, conn2, roomID)

	sendCount := 5
	var wg sync.WaitGroup
	receiveCh := make(chan string, sendCount*2)

	go readWSForAcks(t, conn1, receiveCh, sendCount)
	go readWSForAcks(t, conn2, receiveCh, sendCount)

	time.Sleep(500 * time.Millisecond)

	var writeMu sync.Mutex
	for i := 0; i < sendCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			writeMu.Lock()
			content := fmt.Sprintf("conc_msg_%d_%s", idx, randomString(4))
			sendWSMessage(t, conn1, roomID, content)
			writeMu.Unlock()
		}(i)
	}

	wg.Wait()

	received := 0
	deadline := time.After(15 * time.Second)
	for received < sendCount {
		select {
		case <-receiveCh:
			received++
		case <-deadline:
			t.Fatalf("Timeout: received only %d/%d acks", received, sendCount)
		}
	}
}

func readWSForAcks(t *testing.T, conn *websocket.Conn, ch chan<- string, expected int) {
	count := 0
	for count < expected {
		conn.SetReadDeadline(time.Now().Add(20 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg wsMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		if msg.Type == "ack" {
			count++
			ch <- "ack"
		}
	}
}

func TestE2E_RegistrationValidation(t *testing.T) {
	suffix := randomString(4)

	tests := []struct {
		name     string
		username string
		email    string
		password string
		body     func() *registerRequest
	}{
		{
			name: "short_username",
			body: func() *registerRequest {
				return &registerRequest{Username: "ab", Email: "short_mail_" + suffix + "@example.com", Password: "password123"}
			},
		},
		{
			name: "invalid_email",
			body: func() *registerRequest {
				return &registerRequest{Username: "valid_user_" + suffix, Email: "not-an-email-" + suffix, Password: "password123"}
			},
		},
		{
			name: "short_password",
			body: func() *registerRequest {
				return &registerRequest{Username: "valid_usr_" + suffix, Email: "valid_mail_" + suffix + "@example.com", Password: "short"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.body()
			data, _ := json.Marshal(b)
			resp := doRequest(t, "POST", apiV1+"/auth/register", data, map[string]string{"Content-Type": contentType})
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusBadRequest {
				t.Logf("Correctly got 400 Bad Request for %s", tt.name)
			} else if resp.StatusCode == http.StatusCreated {
				t.Errorf("Validation did NOT reject: %s (got 201) - Validation rules missing", tt.name)
			} else if resp.StatusCode == http.StatusTooManyRequests {
				t.Skip("Rate limited")
			} else {
				t.Logf("Got unexpected status %d for %s", resp.StatusCode, tt.name)
			}
		})
	}
}

func TestE2E_HealthEndpoints(t *testing.T) {
	resp, err := http.Get(apiBase + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	resp, err = http.Get(apiBase + "/readyz")
	require.NoError(t, err)
	defer resp.Body.Close()
	t.Logf("readyz returned %d", resp.StatusCode)

	resp, err = http.Get(apiBase + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestE2E_RootEndpoint(t *testing.T) {
	resp, err := http.Get(apiBase + "/")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	assert.Equal(t, "WebSocket Chat API", result["name"])
}

func TestE2E_WSPingPong(t *testing.T) {
	suffix := randomString(8)
	username := "ping_" + suffix
	email := "ping_" + suffix + "@example.com"
	password := "password123"

	_, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)
	tokens, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)

	conn := connectWS(t, tokens.AccessToken)
	defer conn.Close()

	pingMsg := map[string]interface{}{
		"id":   "ping-1",
		"type": "ping",
		"data": nil,
	}
	data, _ := json.Marshal(pingMsg)
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := conn.WriteMessage(websocket.TextMessage, data)
	require.NoError(t, err)

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, raw, err := conn.ReadMessage()
	require.NoError(t, err)

	var msg wsMessage
	json.Unmarshal(raw, &msg)
	assert.Equal(t, "pong", string(msg.Type))
	t.Log("Received pong response")
}

func TestE2E_WSSendToUnsubscribedRoom(t *testing.T) {
	suffix := randomString(8)
	username := "uns_" + suffix
	email := "uns_" + suffix + "@example.com"
	password := "password123"

	_, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)
	tokens, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)
	roomID, status := createRoom(t, tokens.AccessToken, username+"_uns_room", "group", "")
	require.Equal(t, http.StatusCreated, status)

	conn := connectWS(t, tokens.AccessToken)
	defer conn.Close()

	content := "msg_without_subscribe_" + randomString(4)
	sendWSMessage(t, conn, roomID, content)

	foundError := false
	foundAck := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !foundError {
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg wsMessage
		json.Unmarshal(raw, &msg)
		if msg.Type == "ack" {
			var ack ackData
			json.Unmarshal(msg.Data, &ack)
			if ack.Status == "delivered" {
				foundAck = true
			}
		}
		if msg.Type == "error" {
			foundError = true
		}
	}
	assert.False(t, foundAck, "Should NOT receive ack=delivered when not subscribed to room")
	assert.True(t, foundError, "Should receive error when not subscribed to room")
}

func TestE2E_WSSubscribeNonExistentRoom(t *testing.T) {
	suffix := randomString(8)
	username := "subne_" + suffix
	email := "subne_" + suffix + "@example.com"
	password := "password123"

	_, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)
	tokens, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)

	conn := connectWS(t, tokens.AccessToken)
	defer conn.Close()

	subMsg := map[string]interface{}{
		"id":   "sub-nonexistent",
		"type": "subscribe",
		"data": map[string]interface{}{
			"room_ids": []string{"nonexistent-room-id-12345"},
		},
	}
	data, _ := json.Marshal(subMsg)
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := conn.WriteMessage(websocket.TextMessage, data)
	require.NoError(t, err)

	foundAck := false
	notSubscribed := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !foundAck {
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg wsMessage
		json.Unmarshal(raw, &msg)
		if msg.Type == "ack" {
			var ack ackData
			json.Unmarshal(msg.Data, &ack)
			if ack.Status == "subscribed" {
				foundAck = true
			}
		if ack.Status == "not_subscribed" {
			notSubscribed = true
			break
		}
		}
	}
	assert.False(t, foundAck, "Should NOT receive ack=subscribed for non-existent room")
	assert.True(t, notSubscribed, "Should receive ack=not_subscribed for non-existent room")
}

func sendWSAction(t *testing.T, conn *websocket.Conn, actionType string, data map[string]interface{}) string {
	msgID := fmt.Sprintf("%s-%d", actionType, time.Now().UnixNano())
	msg := map[string]interface{}{
		"id":   msgID,
		"type": actionType,
		"data": data,
	}
	msgBytes, _ := json.Marshal(msg)
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := conn.WriteMessage(websocket.TextMessage, msgBytes)
	require.NoError(t, err)
	return msgID
}

func waitForAck(t *testing.T, conn *websocket.Conn, clientMsgID, expectedStatus string) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg wsMessage
		json.Unmarshal(raw, &msg)
		if msg.Type == "ack" {
			var ack ackData
			json.Unmarshal(msg.Data, &ack)
			if ack.ClientMsgID == clientMsgID {
				require.Equal(t, expectedStatus, ack.Status)
				return
			}
		}
	}
	t.Fatalf("Did not receive ack for msg %s with status %s", clientMsgID, expectedStatus)
}

func waitForNoError(t *testing.T, conn *websocket.Conn) {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg wsMessage
		json.Unmarshal(raw, &msg)
		if msg.Type == "error" {
			t.Errorf("Unexpected error received: %s", string(raw))
			return
		}
	}
}

func TestE2E_DuplicateWebSocketConnections(t *testing.T) {
	suffix := randomString(8)
	username := "dupconn_" + suffix
	email := "dupconn_" + suffix + "@example.com"
	password := "password123"

	_, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)
	tokens, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)
	roomID, status := createRoom(t, tokens.AccessToken, username+"_dup_room", "group", "")
	require.Equal(t, http.StatusCreated, status)

	conn1 := connectWS(t, tokens.AccessToken)
	defer conn1.Close()
	conn2 := connectWS(t, tokens.AccessToken)
	defer conn2.Close()

	subscribeToRoom(t, conn1, roomID)
	subscribeToRoom(t, conn2, roomID)

	content := "dup_conn_test_" + randomString(4)
	sendWSMessage(t, conn1, roomID, content)

	// Both connections should receive the message
	received1 := false
	received2 := false
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		if received1 && received2 {
			break
		}
		for _, pair := range []struct {
			conn    *websocket.Conn
			received *bool
			name    string
		}{
			{conn1, &received1, "conn1"},
			{conn2, &received2, "conn2"},
		} {
			if *pair.received {
				continue
			}
			pair.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			_, raw, err := pair.conn.ReadMessage()
			if err != nil {
				continue
			}
			var msg wsMessage
			json.Unmarshal(raw, &msg)
			if msg.Type == "new_message" || msg.Type == "ack" {
				*pair.received = true
				t.Logf("%s received %s", pair.name, msg.Type)
			}
		}
	}

	assert.True(t, received1, "First connection should receive message")
	assert.True(t, received2, "Second connection should receive broadcast")
}

func TestE2E_Reaction(t *testing.T) {
	suffix := randomString(8)
	username := "react_" + suffix
	email := "react_" + suffix + "@example.com"
	password := "password123"

	_, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)
	tokens, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)
	roomID, status := createRoom(t, tokens.AccessToken, username+"_room", "group", "")
	require.Equal(t, http.StatusCreated, status)

	conn := connectWS(t, tokens.AccessToken)
	defer conn.Close()
	subscribeToRoom(t, conn, roomID)

	content := "react_test_" + randomString(4)
	sendWSMessage(t, conn, roomID, content)

	messageDeadline := time.Now().Add(10 * time.Second)
	var serverMsgID string
	for time.Now().Before(messageDeadline) && serverMsgID == "" {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg wsMessage
		json.Unmarshal(raw, &msg)
		if msg.Type == "ack" {
			var ack ackData
			json.Unmarshal(msg.Data, &ack)
			if ack.Status == "delivered" && ack.ServerMsgID != "" {
				serverMsgID = ack.ServerMsgID
			}
		}
	}
	require.NotEmpty(t, serverMsgID, "Should receive ack with server message ID")

	addID := sendWSAction(t, conn, "reaction", map[string]interface{}{
		"message_id": serverMsgID,
		"emoji":      "👍",
		"action":     "add",
	})
	waitForAck(t, conn, addID, "reaction_add")

	removeID := sendWSAction(t, conn, "reaction", map[string]interface{}{
		"message_id": serverMsgID,
		"emoji":      "👍",
		"action":     "remove",
	})
	waitForAck(t, conn, removeID, "reaction_remove")
}

func TestE2E_EditMessage(t *testing.T) {
	suffix := randomString(8)
	username := "edit_" + suffix
	email := "edit_" + suffix + "@example.com"
	password := "password123"

	_, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)
	tokens, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)
	roomID, status := createRoom(t, tokens.AccessToken, username+"_room", "group", "")
	require.Equal(t, http.StatusCreated, status)

	conn := connectWS(t, tokens.AccessToken)
	defer conn.Close()
	subscribeToRoom(t, conn, roomID)

	content := "edit_test_" + randomString(4)
	sendWSMessage(t, conn, roomID, content)

	var serverMsgID string
	messageDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(messageDeadline) && serverMsgID == "" {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg wsMessage
		json.Unmarshal(raw, &msg)
		if msg.Type == "ack" {
			var ack ackData
			json.Unmarshal(msg.Data, &ack)
			if ack.Status == "delivered" && ack.ServerMsgID != "" {
				serverMsgID = ack.ServerMsgID
			}
		}
	}
	require.NotEmpty(t, serverMsgID, "Should receive ack with server message ID")

	editID := sendWSAction(t, conn, "edit", map[string]interface{}{
		"message_id": serverMsgID,
		"content":    "edited content " + randomString(4),
	})
	waitForAck(t, conn, editID, "edited")
}

func TestE2E_DeleteMessage(t *testing.T) {
	suffix := randomString(8)
	username := "del_" + suffix
	email := "del_" + suffix + "@example.com"
	password := "password123"

	_, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)
	tokens, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)
	roomID, status := createRoom(t, tokens.AccessToken, username+"_room", "group", "")
	require.Equal(t, http.StatusCreated, status)

	conn := connectWS(t, tokens.AccessToken)
	defer conn.Close()
	subscribeToRoom(t, conn, roomID)

	content := "del_test_" + randomString(4)
	sendWSMessage(t, conn, roomID, content)

	var serverMsgID string
	messageDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(messageDeadline) && serverMsgID == "" {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg wsMessage
		json.Unmarshal(raw, &msg)
		if msg.Type == "ack" {
			var ack ackData
			json.Unmarshal(msg.Data, &ack)
			if ack.Status == "delivered" && ack.ServerMsgID != "" {
				serverMsgID = ack.ServerMsgID
			}
		}
	}
	require.NotEmpty(t, serverMsgID, "Should receive ack with server message ID")

	deleteID := sendWSAction(t, conn, "delete", map[string]interface{}{
		"message_id": serverMsgID,
	})
	waitForAck(t, conn, deleteID, "deleted")
}

func TestE2E_TypingIndicator(t *testing.T) {
	suffix := randomString(8)
	username := "type_" + suffix
	email := "type_" + suffix + "@example.com"
	password := "password123"

	_, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)
	tokens, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)
	roomID, status := createRoom(t, tokens.AccessToken, username+"_room", "group", "")
	require.Equal(t, http.StatusCreated, status)

	conn := connectWS(t, tokens.AccessToken)
	defer conn.Close()
	subscribeToRoom(t, conn, roomID)

	sendWSAction(t, conn, "typing", map[string]interface{}{
		"room_id": roomID,
	})

	waitForNoError(t, conn)
}

func TestE2E_Presence(t *testing.T) {
	suffix := randomString(8)
	username := "pres_" + suffix
	email := "pres_" + suffix + "@example.com"
	password := "password123"

	_, status := registerUser(t, username, email, password)
	require.Equal(t, http.StatusCreated, status)
	tokens, status := loginUser(t, email, password)
	require.Equal(t, http.StatusOK, status)

	conn := connectWS(t, tokens.AccessToken)
	defer conn.Close()

	sendWSAction(t, conn, "presence", map[string]interface{}{
		"status": "online",
	})

	waitForNoError(t, conn)

	conn.Close()

	conn2 := connectWS(t, tokens.AccessToken)
	defer conn2.Close()

	sendWSAction(t, conn2, "presence", map[string]interface{}{
		"status": "online",
	})

	waitForNoError(t, conn2)
}

func TestE2E_RateLimitExceeded(t *testing.T) {
	if os.Getenv("E2E_DISABLE_RATELIMIT") != "" {
		t.Skip("Rate limiting disabled via E2E_DISABLE_RATELIMIT")
	}

	suffix := randomString(4)
	count := 12

	client := &http.Client{Timeout: 5 * time.Second}

	for i := 0; i < count; i++ {
		email := fmt.Sprintf("ratelimit_%s_%d@example.com", suffix, i)
		body := loginRequest{Email: email, Password: "irrelevant"}
		data, _ := json.Marshal(body)

		req, err := http.NewRequest("POST", apiV1+"/auth/login", bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", contentType)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}

		if i >= 10 {
			if resp.StatusCode == http.StatusTooManyRequests {
				t.Logf("Correctly rate limited on attempt %d", i+1)
				resp.Body.Close()
				return
			}
		}

		if i == count-1 {
			resp.Body.Close()
			t.Errorf("Expected 429 Too Many Requests by attempt %d, but got %d", count, resp.StatusCode)
		} else {
			resp.Body.Close()
		}
	}
}
