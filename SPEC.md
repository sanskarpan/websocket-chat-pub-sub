# WebSocket Chat System with Pub-Sub
## Production-Ready Real-Time Communication Platform

---

## Executive Summary

Build a **production-grade WebSocket Chat System** that combines real-time messaging, room-based chat, presence detection, message persistence, and a scalable pub-sub architecture. This system should handle 10,000+ concurrent connections per node, with horizontal scaling capabilities through Redis Pub-Sub.

### Key Differentiators
- **Enterprise-grade reliability**: Message delivery guarantees, automatic reconnection
- **Observability first**: Distributed tracing, structured logging, comprehensive metrics
- **Security by design**: JWT authentication, rate limiting, message encryption at rest
- **Horizontal scaling**: Stateless WebSocket servers with Redis backplane

---

## 1. Architecture Overview

### System Architecture
```
┌─────────────────────────────────────────────────────────────────┐
│                        Load Balancer                             │
│                   (Sticky Sessions / IP Hash)                    │
└──────────────────────────────┬──────────────────────────────────┘
                               │
         ┌─────────────────────┼─────────────────────┐
         │                     │                     │
         ▼                     ▼                     ▼
┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐
│  WebSocket      │   │  WebSocket      │   │  WebSocket      │
│  Server 1       │◄──│  Server 2       │◄──│  Server 3       │
│  (Stateless)    │   │  (Stateless)    │   │  (Stateless)    │
└────────┬────────┘   └────────┬────────┘   └────────┬────────┘
         │                     │                     │
         └─────────────────────┼─────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Redis Cluster (Pub-Sub)                       │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐           │
│  │   Master     │  │   Master     │  │   Master     │           │
│  │   + Replica  │  │   + Replica  │  │   + Replica  │           │
│  └──────────────┘  └──────────────┘  └──────────────┘           │
└──────────────────────────────┬──────────────────────────────────┘
                               │
         ┌─────────────────────┼─────────────────────┐
         │                     │                     │
         ▼                     ▼                     ▼
┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐
│  PostgreSQL     │   │  ClickHouse/    │   │  MinIO/S3       │
│  (Primary DB)   │   │  TimescaleDB    │   │  (File Storage) │
│                 │   │  (Analytics)    │   │                 │
└─────────────────┘   └─────────────────┘   └─────────────────┘
```

### Communication Patterns
1. **Client → Server**: WebSocket connection with JWT auth
2. **Server → Server**: Redis Pub/Sub for cross-node messaging
3. **Server → Persistence**: Async write to PostgreSQL with write-behind caching
4. **Analytics Pipeline**: Message metadata to ClickHouse for analytics

---

## 2. Technology Stack

### Core Technologies
| Component | Technology | Justification |
|-----------|-----------|---------------|
| Language | **Go 1.21+** | Performance, concurrency, memory efficiency |
| WebSocket Library | **gorilla/websocket** or **nhooyr/websocket** | Battle-tested, feature-rich |
| HTTP Framework | **fasthttp** or **gin** | High performance, middleware support |
| Database | **PostgreSQL 15+** | ACID compliance, JSON support, replication |
| Cache | **Redis 7+ Cluster** | Pub-Sub, presence, rate limiting, session store |
| Message Queue | **Redis Streams** or **NATS** | Async processing, backpressure handling |
| Object Storage | **MinIO** (dev) / **S3** (prod) | File attachments |
| Time-Series DB | **ClickHouse** or **TimescaleDB** | Message analytics, metrics aggregation |

### Observability Stack
| Component | Technology |
|-----------|-----------|
| Metrics | **Prometheus** + **Grafana** |
| Logging | **zap** (structured) + **Loki** (aggregation) |
| Tracing | **OpenTelemetry** + **Jaeger** |
| APM | **Jaeger** or **Tempo** |

### DevOps & Deployment
| Component | Technology |
|-----------|-----------|
| Containerization | **Docker** + **Docker Compose** (local) |
| Orchestration | **Kubernetes** (Helm charts) |
| CI/CD | **GitHub Actions** |
| Infrastructure | **Terraform** (optional) |

---

## 3. Functional Requirements

### Core Chat Features

#### 3.1 User Management
```go
type User struct {
    ID            string    `json:"id" db:"id"`                    // UUID v4
    Username      string    `json:"username" db:"username"`        // Unique, 3-30 chars
    Email         string    `json:"email" db:"email"`              // Validated email
    DisplayName   string    `json:"display_name" db:"display_name"`
    AvatarURL     string    `json:"avatar_url" db:"avatar_url"`
    Status        UserStatus `json:"status" db:"status"`           // online, away, dnd, offline
    LastSeenAt    time.Time `json:"last_seen_at" db:"last_seen_at"`
    CreatedAt     time.Time `json:"created_at" db:"created_at"`
    UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
    Metadata      JSONB     `json:"metadata" db:"metadata"`        // Flexible user data
}

type UserStatus string
const (
    StatusOnline  UserStatus = "online"
    StatusAway    UserStatus = "away"
    StatusDND     UserStatus = "dnd"
    StatusOffline UserStatus = "offline"
)
```

#### 3.2 Room Management
```go
type Room struct {
    ID            string         `json:"id" db:"id"`                    // UUID v4
    Name          string         `json:"name" db:"name"`                // Room name
    Type          RoomType       `json:"type" db:"type"`                // direct, group, channel
    Description   string         `json:"description" db:"description"`
    AvatarURL     string         `json:"avatar_url" db:"avatar_url"`
    CreatedBy     string         `json:"created_by" db:"created_by"`    // User ID
    CreatedAt     time.Time      `json:"created_at" db:"created_at"`
    UpdatedAt     time.Time      `json:"updated_at" db:"updated_at"`
    ArchivedAt    *time.Time     `json:"archived_at,omitempty" db:"archived_at"`
    Settings      RoomSettings   `json:"settings" db:"settings"`        // JSONB
    MemberCount   int            `json:"member_count" db:"member_count"`
}

type RoomType string
const (
    RoomTypeDirect  RoomType = "direct"   // 1-on-1 chat
    RoomTypeGroup   RoomType = "group"    // Private group
    RoomTypeChannel RoomType = "channel"  // Public channel
)

type RoomSettings struct {
    AllowReactions      bool   `json:"allow_reactions"`
    AllowThreads        bool   `json:"allow_threads"`
    MessageRetention    int    `json:"message_retention_days"` // 0 = forever
    SlowModeSeconds     int    `json:"slow_mode_seconds"`      // Min time between messages
    RequireApproval     bool   `json:"require_approval"`       // For join requests
    OnlyAdminsCanPost   bool   `json:"only_admins_can_post"`
}
```

#### 3.3 Membership & Permissions
```go
type RoomMember struct {
    RoomID      string     `json:"room_id" db:"room_id"`
    UserID      string     `json:"user_id" db:"user_id"`
    Role        MemberRole `json:"role" db:"role"`           // owner, admin, moderator, member
    JoinedAt    time.Time  `json:"joined_at" db:"joined_at"`
    LeftAt      *time.Time `json:"left_at,omitempty" db:"left_at"`
    LastReadAt  time.Time  `json:"last_read_at" db:"last_read_at"`
    MutedUntil  *time.Time `json:"muted_until,omitempty" db:"muted_until"`
    BannedAt    *time.Time `json:"banned_at,omitempty" db:"banned_at"`
    BanReason   string     `json:"ban_reason,omitempty" db:"ban_reason"`
    Notifications NotificationSettings `json:"notifications" db:"notifications"`
}

type MemberRole string
const (
    RoleOwner     MemberRole = "owner"
    RoleAdmin     MemberRole = "admin"
    RoleModerator MemberRole = "moderator"
    RoleMember    MemberRole = "member"
)

type NotificationSettings struct {
    Enabled     bool   `json:"enabled"`
    MentionsOnly bool  `json:"mentions_only"`
    Sound       bool   `json:"sound"`
}
```

#### 3.4 Message Model
```go
type Message struct {
    ID              string         `json:"id" db:"id"`                    // Snowflake ID
    RoomID          string         `json:"room_id" db:"room_id"`
    UserID          string         `json:"user_id" db:"user_id"`
    Content         string         `json:"content" db:"content"`
    ContentType     ContentType    `json:"content_type" db:"content_type"` // text, markdown, system
    ParentID        *string        `json:"parent_id,omitempty" db:"parent_id"` // Thread parent
    ThreadCount     int            `json:"thread_count" db:"thread_count"`
    
    // Message state
    EditedAt        *time.Time     `json:"edited_at,omitempty" db:"edited_at"`
    DeletedAt       *time.Time     `json:"deleted_at,omitempty" db:"deleted_at"`
    DeletedBy       *string        `json:"deleted_by,omitempty" db:"deleted_by"`
    
    // Reactions (stored as JSONB for performance)
    Reactions       map[string][]string `json:"reactions" db:"reactions"` // emoji -> []userIDs
    
    // Attachments
    Attachments     []Attachment   `json:"attachments" db:"attachments"`
    
    // Metadata
    Metadata        MessageMetadata `json:"metadata" db:"metadata"`
    
    // Timestamps
    CreatedAt       time.Time      `json:"created_at" db:"created_at"`
    ClientTimestamp *time.Time     `json:"client_timestamp,omitempty" db:"client_timestamp"` // For ordering
}

type ContentType string
const (
    ContentTypeText     ContentType = "text"
    ContentTypeMarkdown ContentType = "markdown"
    ContentTypeSystem   ContentType = "system"      // Join/leave messages
    ContentTypeFile     ContentType = "file"
)

type Attachment struct {
    ID          string `json:"id"`
    FileName    string `json:"file_name"`
    FileSize    int64  `json:"file_size"`
    MimeType    string `json:"mime_type"`
    URL         string `json:"url"`
    ThumbnailURL string `json:"thumbnail_url,omitempty"`
    Width       int    `json:"width,omitempty"`     // For images
    Height      int    `json:"height,omitempty"`
}

type MessageMetadata struct {
    ClientID        string            `json:"client_id,omitempty"`        // For deduplication
    ReplyTo         *string           `json:"reply_to,omitempty"`
    ForwardedFrom   *ForwardedInfo    `json:"forwarded_from,omitempty"`
    CustomData      map[string]interface{} `json:"custom_data,omitempty"`
}

type ForwardedInfo struct {
    RoomID    string    `json:"room_id"`
    MessageID string    `json:"message_id"`
    UserID    string    `json:"user_id"`
    At        time.Time `json:"at"`
}
```

### WebSocket Protocol

#### Connection Flow
```
1. Client authenticates via HTTP API, receives JWT
2. Client opens WebSocket: wss://api.example.com/ws?token=<jwt>
3. Server validates JWT, creates connection
4. Server sends: {"type": "connection", "data": {"user_id": "...", "session_id": "..."}}
5. Client subscribes to rooms: {"type": "subscribe", "data": {"room_ids": ["..."]}}
6. Server confirms with room state
```

#### Message Types
```go
// Client → Server
type ClientMessage struct {
    ID        string          `json:"id"`        // Client-generated ID for dedup
    Type      ClientMsgType   `json:"type"`
    Timestamp time.Time       `json:"timestamp"`
    Data      json.RawMessage `json:"data"`
}

type ClientMsgType string
const (
    ClientMsgSubscribe      ClientMsgType = "subscribe"       // Subscribe to rooms
    ClientMsgUnsubscribe    ClientMsgType = "unsubscribe"     // Unsubscribe
    ClientMsgMessage        ClientMsgType = "message"         // Send message
    ClientMsgTyping         ClientMsgType = "typing"          // Typing indicator
    ClientMsgReadReceipt    ClientMsgType = "read_receipt"    // Mark as read
    ClientMsgReaction       ClientMsgType = "reaction"        // Add/remove reaction
    ClientMsgEdit           ClientMsgType = "edit"            // Edit message
    ClientMsgDelete         ClientMsgType = "delete"          // Delete message
    ClientMsgPresence       ClientMsgType = "presence"        // Update presence
    ClientMsgPing           ClientMsgType = "ping"            // Keep-alive
)

// Server → Client
type ServerMessage struct {
    ID        string          `json:"id"`        // Server-generated ID
    Type      ServerMsgType   `json:"type"`
    Timestamp time.Time       `json:"timestamp"`
    Data      json.RawMessage `json:"data"`
}

type ServerMsgType string
const (
    ServerMsgConnection     ServerMsgType = "connection"      // Connection established
    ServerMsgAck            ServerMsgType = "ack"             // Message acknowledged
    ServerMsgError          ServerMsgType = "error"           // Error response
    ServerMsgNewMessage     ServerMsgType = "new_message"     // New chat message
    ServerMsgMessageUpdated ServerMsgType = "message_updated" // Edited/deleted
    ServerMsgTyping         ServerMsgType = "typing"          // User typing
    ServerMsgPresence       ServerMsgType = "presence"        // User presence update
    ServerMsgReadReceipt    ServerMsgType = "read_receipt"    // Read status
    ServerMsgReaction       ServerMsgType = "reaction"        // Reaction update
    ServerMsgRoomUpdated    ServerMsgType = "room_updated"    // Room metadata changed
    ServerMsgMemberJoined   ServerMsgType = "member_joined"   // New member
    ServerMsgMemberLeft     ServerMsgType = "member_left"     // Member left
    ServerMsgSystem         ServerMsgType = "system"          // System message
    ServerMsgPong           ServerMsgType = "pong"            // Keep-alive response
)
```

### Presence System
```go
type Presence struct {
    UserID      string     `json:"user_id"`
    Status      UserStatus `json:"status"`     // online, away, dnd, offline
    LastActive  time.Time  `json:"last_active"`
    CurrentRoom *string    `json:"current_room,omitempty"` // Currently viewing
    ClientInfo  ClientInfo `json:"client_info"`
}

type ClientInfo struct {
    Platform    string `json:"platform"`     // web, ios, android, desktop
    Version     string `json:"version"`
    DeviceID    string `json:"device_id"`
}

// Presence tracking via Redis with expiry
// Key: presence:<user_id>
// TTL: 5 minutes (refreshed on activity)
```

---

## 4. Non-Functional Requirements

### Performance Targets
| Metric | Target | Notes |
|--------|--------|-------|
| Connection Setup | < 100ms | WebSocket handshake + auth |
| Message Latency | < 50ms | P99 for same-region delivery |
| Concurrent Connections | 10,000/node | Per WebSocket server |
| Message Throughput | 50,000/sec | Per cluster |
| DB Write Latency | < 10ms | P99 for message persistence |
| API Response Time | < 50ms | P99 for REST endpoints |

### Scalability Requirements
- **Horizontal Scaling**: Stateless WebSocket servers, Redis as backplane
- **Database Sharding**: Support sharding by room_id for messages table
- **Read Replicas**: Use read replicas for message history queries
- **Caching Strategy**: Redis for hot data (presence, recent messages, room lists)

### Reliability & Availability
- **SLA Target**: 99.99% uptime (52 minutes downtime/year)
- **Message Durability**: Zero message loss (async persistence with retry)
- **Connection Resilience**: Automatic reconnection with message replay
- **Graceful Degradation**: Continue operating if analytics DB is down

### Security Requirements
- **Authentication**: JWT with RS256, 15-minute access tokens, 7-day refresh tokens
- **Transport**: TLS 1.3 for all connections
- **Rate Limiting**:
  - 100 messages/minute per user
  - 10 rooms/minute creation
  - 5 connections/minute per IP
- **Message Sanitization**: XSS protection, link validation
- **Data Encryption**: 
  - At rest: AES-256 for database
  - In transit: TLS 1.3
  - Sensitive fields: Application-level encryption for PII

### Observability Requirements
- **Metrics** (Prometheus):
  - Connection count (gauge)
  - Message throughput (counter)
  - Latency histograms (P50, P95, P99)
  - Error rates by type
  - Redis operation latencies
  - DB connection pool stats
- **Logging** (Structured JSON):
  - Every WebSocket event
  - Every DB operation (slow query log > 100ms)
  - Authentication events
  - Error stack traces
- **Tracing** (OpenTelemetry):
  - Trace ID propagated through all services
  - Spans for: WS handler, DB queries, Redis ops, Pub-Sub publish
  - Sampling: 1% normal, 100% on error

---

## 5. API Design

### REST API Endpoints

#### Authentication
```
POST   /api/v1/auth/register          # User registration
POST   /api/v1/auth/login             # User login
POST   /api/v1/auth/refresh           # Refresh token
POST   /api/v1/auth/logout            # Logout (invalidate tokens)
DELETE /api/v1/auth/sessions          # Logout all sessions
```

#### Users
```
GET    /api/v1/users/me               # Get current user
PATCH  /api/v1/users/me               # Update profile
POST   /api/v1/users/me/avatar        # Upload avatar
GET    /api/v1/users/:id              # Get user by ID
GET    /api/v1/users/search?q=...     # Search users
GET    /api/v1/users/:id/presence     # Get user presence
```

#### Rooms
```
GET    /api/v1/rooms                  # List my rooms (with unread counts)
POST   /api/v1/rooms                  # Create room
GET    /api/v1/rooms/:id              # Get room details
PATCH  /api/v1/rooms/:id              # Update room
DELETE /api/v1/rooms/:id              # Archive/Delete room
GET    /api/v1/rooms/:id/members      # List members
POST   /api/v1/rooms/:id/members      # Add member (invitation)
DELETE /api/v1/rooms/:id/members/:user_id  # Remove member
PATCH  /api/v1/rooms/:id/members/:user_id  # Update member role
POST   /api/v1/rooms/:id/join         # Join public room
POST   /api/v1/rooms/:id/leave        # Leave room
```

#### Messages
```
GET    /api/v1/rooms/:id/messages     # Get messages (cursor pagination)
POST   /api/v1/rooms/:id/messages     # Send message (REST fallback)
GET    /api/v1/rooms/:id/messages/:msg_id        # Get specific message
PATCH  /api/v1/rooms/:id/messages/:msg_id        # Edit message
DELETE /api/v1/rooms/:id/messages/:msg_id        # Delete message
POST   /api/v1/rooms/:id/messages/:msg_id/thread # Get thread replies
GET    /api/v1/rooms/:id/messages/search?q=...   # Search messages
```

#### Files
```
POST   /api/v1/files/upload           # Get presigned upload URL
GET    /api/v1/files/:id              # Download file (with auth)
```

### WebSocket Events Detail

#### Client Subscribe
```json
{
  "id": "client-msg-001",
  "type": "subscribe",
  "timestamp": "2024-01-15T10:30:00Z",
  "data": {
    "room_ids": ["room-uuid-1", "room-uuid-2"],
    "presence_subscribe": ["user-uuid-1", "user-uuid-2"]
  }
}
```

#### Server Ack
```json
{
  "id": "server-msg-001",
  "type": "ack",
  "timestamp": "2024-01-15T10:30:00.050Z",
  "data": {
    "client_msg_id": "client-msg-001",
    "rooms": [
      {
        "room_id": "room-uuid-1",
        "last_message": { /* message object */ },
        "unread_count": 5,
        "members_online": ["user-uuid-3"]
      }
    ]
  }
}
```

#### New Message
```json
{
  "id": "server-msg-042",
  "type": "new_message",
  "timestamp": "2024-01-15T10:30:05Z",
  "data": {
    "room_id": "room-uuid-1",
    "message": {
      "id": "msg-snowflake-id",
      "room_id": "room-uuid-1",
      "user_id": "user-uuid-1",
      "content": "Hello everyone!",
      "content_type": "text",
      "created_at": "2024-01-15T10:30:05Z",
      "user": {
        "id": "user-uuid-1",
        "username": "johndoe",
        "display_name": "John Doe",
        "avatar_url": "https://..."
      }
    }
  }
}
```

---

## 6. Database Schema

### PostgreSQL Schema

#### Users Table
```sql
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(30) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,  -- bcrypt
    display_name VARCHAR(100),
    avatar_url TEXT,
    status VARCHAR(20) DEFAULT 'offline',
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    metadata JSONB DEFAULT '{}',
    
    CONSTRAINT username_format CHECK (username ~ '^[a-zA-Z0-9_-]{3,30}$'),
    CONSTRAINT email_format CHECK (email ~* '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$')
);

CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_status ON users(status) WHERE status != 'offline';
```

#### Rooms Table
```sql
CREATE TABLE rooms (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    type VARCHAR(20) NOT NULL CHECK (type IN ('direct', 'group', 'channel')),
    description TEXT,
    avatar_url TEXT,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    archived_at TIMESTAMPTZ,
    settings JSONB DEFAULT '{
        "allow_reactions": true,
        "allow_threads": true,
        "message_retention_days": 0,
        "slow_mode_seconds": 0,
        "require_approval": false,
        "only_admins_can_post": false
    }',
    member_count INT DEFAULT 0,
    
    -- For direct rooms, ensure uniqueness
    CONSTRAINT unique_direct_room UNIQUE NULLS NOT DISTINCT (type, name) 
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX idx_rooms_type ON rooms(type);
CREATE INDEX idx_rooms_created_by ON rooms(created_by);
CREATE INDEX idx_rooms_archived ON rooms(archived_at) WHERE archived_at IS NULL;
```

#### Room Members Table
```sql
CREATE TABLE room_members (
    room_id UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(20) NOT NULL DEFAULT 'member' 
        CHECK (role IN ('owner', 'admin', 'moderator', 'member')),
    joined_at TIMESTAMPTZ DEFAULT NOW(),
    left_at TIMESTAMPTZ,
    last_read_at TIMESTAMPTZ DEFAULT NOW(),
    muted_until TIMESTAMPTZ,
    banned_at TIMESTAMPTZ,
    ban_reason TEXT,
    notifications JSONB DEFAULT '{"enabled": true, "mentions_only": false, "sound": true}',
    
    PRIMARY KEY (room_id, user_id)
);

CREATE INDEX idx_room_members_user ON room_members(user_id) WHERE left_at IS NULL;
CREATE INDEX idx_room_members_room ON room_members(room_id) WHERE left_at IS NULL;
CREATE INDEX idx_room_members_banned ON room_members(room_id, banned_at) WHERE banned_at IS NOT NULL;
```

#### Messages Table (Partitioned by time)
```sql
CREATE TABLE messages (
    id BIGINT NOT NULL,  -- Snowflake ID
    room_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id),
    content TEXT NOT NULL,
    content_type VARCHAR(20) DEFAULT 'text',
    parent_id BIGINT,    -- For threads
    thread_count INT DEFAULT 0,
    edited_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    deleted_by UUID REFERENCES users(id),
    reactions JSONB DEFAULT '{}',  -- {"👍": ["user-id-1", "user-id-2"]}
    attachments JSONB DEFAULT '[]',
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    client_timestamp TIMESTAMPTZ,  -- For ordering correction
    
    PRIMARY KEY (id, created_at)  -- Include partition key
) PARTITION BY RANGE (created_at);

-- Create monthly partitions
CREATE TABLE messages_y2024m01 PARTITION OF messages
    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
-- ... create partitions dynamically or use pg_partman

CREATE INDEX idx_messages_room_created ON messages(room_id, created_at DESC);
CREATE INDEX idx_messages_user ON messages(user_id, created_at DESC);
CREATE INDEX idx_messages_parent ON messages(parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX idx_messages_search ON messages USING GIN(to_tsvector('english', content));

-- Function to update thread count
CREATE OR REPLACE FUNCTION update_thread_count()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.parent_id IS NOT NULL THEN
        UPDATE messages 
        SET thread_count = thread_count + 1
        WHERE id = NEW.parent_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER thread_count_trigger
    AFTER INSERT ON messages
    FOR EACH ROW
    EXECUTE FUNCTION update_thread_count();
```

#### Read Receipts Table
```sql
CREATE TABLE read_receipts (
    room_id UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id BIGINT NOT NULL,
    read_at TIMESTAMPTZ DEFAULT NOW(),
    
    PRIMARY KEY (room_id, user_id)
);

CREATE INDEX idx_read_receipts_message ON read_receipts(message_id);
```

#### Files/Attachments Table
```sql
CREATE TABLE files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    room_id UUID REFERENCES rooms(id),
    file_name VARCHAR(255) NOT NULL,
    file_size BIGINT NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    storage_path TEXT NOT NULL,  -- Path in S3/MinIO
    storage_bucket VARCHAR(100) NOT NULL,
    thumbnail_path TEXT,
    width INT,
    height INT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ  -- For temporary uploads
);

CREATE INDEX idx_files_room ON files(room_id);
CREATE INDEX idx_files_user ON files(user_id);
```

### Redis Data Structures

```
# Presence (Hash with expiration)
HSET presence:<user_id> status online last_active <timestamp> client_info <json>
EXPIRE presence:<user_id> 300  # 5 minutes

# User sessions (Set of connection IDs)
SADD user_sessions:<user_id> <conn_id_1> <conn_id_2>

# Room subscriptions (Set of user IDs)
SADD room_subscribers:<room_id> <user_id_1> <user_id_2>

# Recent messages (Sorted Set by timestamp)
ZADD room_messages:<room_id> <timestamp> <message_id>
ZREMRANGEBYSCORE room_messages:<room_id> -inf <cutoff_timestamp>  # Cleanup old

# Rate limiting (Sliding window)
ZADD ratelimit:<user_id>:<action> <timestamp> <timestamp>
ZREMRANGEBYSCORE ratelimit:<user_id>:<action> -inf <window_start>
ZCARD ratelimit:<user_id>:<action>
EXPIRE ratelimit:<user_id>:<action> 60

# Pub-Sub channels
PUBLISH ws:room:<room_id> <message_json>
PUBLISH ws:user:<user_id> <notification_json>

# Message deduplication (for exactly-once delivery)
SETEX msg:<client_msg_id>:<user_id> 60 1
```

---

## 7. Implementation Architecture

### Project Structure
```
websocket-chat/
├── cmd/
│   ├── server/              # Main WebSocket server
│   │   └── main.go
│   ├── api/                 # REST API server (can be separate or same)
│   │   └── main.go
│   ├── worker/              # Background workers
│   │   └── main.go
│   └── migrate/             # Database migrations
│       └── main.go
├── internal/
│   ├── server/              # WebSocket server implementation
│   │   ├── server.go        # Main server struct
│   │   ├── handler.go       # HTTP handlers
│   │   ├── websocket.go     # WebSocket connection management
│   │   ├── hub.go           # Room/subscription management
│   │   └── middleware/
│   │       ├── auth.go
│   │       ├── rate_limit.go
│   │       └── cors.go
│   ├── service/             # Business logic layer
│   │   ├── auth_service.go
│   │   ├── room_service.go
│   │   ├── message_service.go
│   │   ├── user_service.go
│   │   ├── presence_service.go
│   │   └── file_service.go
│   ├── repository/          # Data access layer
│   │   ├── user_repo.go
│   │   ├── room_repo.go
│   │   ├── message_repo.go
│   │   └── file_repo.go
│   ├── model/               # Domain models
│   │   ├── user.go
│   │   ├── room.go
│   │   ├── message.go
│   │   └── presence.go
│   ├── protocol/            # WebSocket protocol definitions
│   │   ├── message.go       # Message types
│   │   └── codec.go         # JSON encoding/decoding
│   ├── pubsub/              # Pub-Sub abstraction
│   │   ├── pubsub.go        # Interface
│   │   ├── redis.go         # Redis implementation
│   │   └── local.go         # In-memory (for single-node)
│   ├── websocket/           # WebSocket primitives
│   │   ├── conn.go          # Connection wrapper
│   │   ├── pool.go          # Connection pool
│   │   └── broadcast.go     # Broadcast utilities
│   ├── auth/                # Authentication
│   │   ├── jwt.go
│   │   ├── middleware.go
│   │   └── password.go
│   ├── cache/               # Caching layer
│   │   ├── cache.go
│   │   └── redis.go
│   ├── metrics/             # Prometheus metrics
│   │   └── metrics.go
│   ├── logging/             # Structured logging
│   │   └── logger.go
│   └── tracing/             # OpenTelemetry tracing
│       └── tracer.go
├── pkg/
│   ├── snowflake/           # ID generation
│   │   └── snowflake.go
│   ├── validator/           # Input validation
│   │   └── validator.go
│   ├── sanitization/        # Content sanitization
│   │   └── xss.go
│   └── errors/              # Error handling
│       └── errors.go
├── api/
│   ├── openapi.yaml         # OpenAPI specification
│   └── proto/               # gRPC definitions (if needed)
├── configs/
│   ├── config.yaml          # Default config
│   ├── config.prod.yaml     # Production config
│   └── config.test.yaml     # Test config
├── deployments/
│   ├── docker/
│   │   ├── Dockerfile.server
│   │   ├── Dockerfile.api
│   │   └── docker-compose.yaml
│   └── kubernetes/
│       ├── deployment.yaml
│       ├── service.yaml
│       ├── ingress.yaml
│       ├── hpa.yaml
│       └── redis/
├── migrations/              # SQL migrations
│   ├── 001_init.sql
│   ├── 002_add_indexes.sql
│   └── ...
├── scripts/
│   ├── generate_keys.sh     # Generate JWT keys
│   ├── run_tests.sh
│   └── benchmark.sh
├── test/
│   ├── integration/         # Integration tests
│   ├── load/                # Load tests (k6)
│   └── fixtures/            # Test data
├── web/                     # Frontend (optional)
│   └── ...
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

### Key Components

#### 1. Connection Manager
```go
type Hub struct {
    // Client management
    clients    map[string]*Client          // conn_id -> Client
    users      map[string]map[string]bool  // user_id -> set of conn_ids
    rooms      map[string]map[string]bool  // room_id -> set of conn_ids
    
    // Channels
    register   chan *Client
    unregister chan *Client
    broadcast  chan *BroadcastMessage
    
    // Dependencies
    pubsub     pubsub.PubSub
    logger     *zap.Logger
    metrics    *Metrics
}

type Client struct {
    hub        *Hub
    conn       *websocket.Conn
    id         string        // Connection ID (UUID)
    userID     string
    rooms      map[string]bool  // Subscribed rooms
    send       chan []byte   // Buffered channel for outbound messages
    
    // State
    lastPing   time.Time
    mu         sync.RWMutex
}

func (c *Client) ReadPump() {
    // Handle incoming messages
    // Parse JSON, validate, dispatch to handlers
}

func (c *Client) WritePump() {
    // Handle outgoing messages
    // Send from channel, handle ping/pong
}
```

#### 2. Pub-Sub Interface
```go
type PubSub interface {
    // Publish message to a channel
    Publish(ctx context.Context, channel string, message []byte) error
    
    // Subscribe to channels
    Subscribe(ctx context.Context, channels ...string) (Subscription, error)
    
    // Presence management
    SetPresence(ctx context.Context, userID string, presence Presence) error
    GetPresence(ctx context.Context, userID string) (*Presence, error)
    GetPresences(ctx context.Context, userIDs []string) (map[string]*Presence, error)
    
    // Room subscriptions
    SubscribeToRoom(ctx context.Context, roomID, userID string) error
    UnsubscribeFromRoom(ctx context.Context, roomID, userID string) error
    GetRoomSubscribers(ctx context.Context, roomID string) ([]string, error)
    
    // Rate limiting
    CheckRateLimit(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}
```

#### 3. Message Processing Pipeline
```
Incoming Message Flow:
1. WebSocket read
2. JSON decode + validation
3. Rate limit check (Redis)
4. Authentication check (JWT)
5. Permission check (room membership)
6. Business logic (service layer)
7. Persist to DB (async with retry)
8. Publish to Redis Pub-Sub
9. ACK to sender
10. Broadcast to room subscribers

Outgoing Message Flow:
1. Receive from Redis Pub-Sub
2. Filter by subscription
3. Serialize
4. Send to client (with timeout)
5. Handle backpressure
```

#### 4. Graceful Shutdown
```go
func (s *Server) Shutdown(ctx context.Context) error {
    // 1. Stop accepting new connections
    s.httpServer.Shutdown(ctx)
    
    // 2. Broadcast shutdown notice to all clients
    s.hub.BroadcastSystemMessage("Server restarting, please reconnect...")
    
    // 3. Wait for clients to disconnect (with timeout)
    s.hub.WaitForDisconnect(ctx)
    
    // 4. Close all WebSocket connections
    s.hub.CloseAll()
    
    // 5. Flush pending messages to DB
    s.messageService.Flush()
    
    // 6. Close Redis connections
    s.pubsub.Close()
    
    return nil
}
```

---

## 8. Configuration

### Config Structure
```yaml
# configs/config.yaml
app:
  name: "websocket-chat"
  version: "1.0.0"
  environment: "development"  # development, staging, production
  
server:
  host: "0.0.0.0"
  port: 8080
  websocket:
    path: "/ws"
    read_buffer_size: 1024
    write_buffer_size: 1024
    max_message_size: 65536  # 64KB
    ping_interval: 30s
    pong_timeout: 60s
    write_timeout: 10s
    max_connections_per_ip: 10
  
  http:
    read_timeout: 30s
    write_timeout: 30s
    idle_timeout: 120s
    max_header_bytes: 1048576  # 1MB
    
  tls:
    enabled: false
    cert_file: ""
    key_file: ""

database:
  postgresql:
    host: "localhost"
    port: 5432
    database: "chat"
    user: "chat"
    password: "${DB_PASSWORD}"  # From env
    ssl_mode: "disable"
    max_open_conns: 25
    max_idle_conns: 10
    conn_max_lifetime: 30m
    
  redis:
    mode: "single"  # single, sentinel, cluster
    addrs:
      - "localhost:6379"
    password: "${REDIS_PASSWORD}"
    db: 0
    pool_size: 10
    min_idle_conns: 5
    
    # For cluster mode
    # mode: "cluster"
    # addrs:
    #   - "redis-node-1:6379"
    #   - "redis-node-2:6379"
    #   - "redis-node-3:6379"

auth:
  jwt:
    algorithm: "RS256"
    private_key: "${JWT_PRIVATE_KEY}"  # PEM encoded
    public_key: "${JWT_PUBLIC_KEY}"
    access_token_ttl: 15m
    refresh_token_ttl: 168h  # 7 days
    issuer: "chat-app"
    audience: ["chat-api"]
  
  bcrypt:
    cost: 12

rate_limit:
  enabled: true
  rules:
    - key: "message"
      limit: 100
      window: 1m
    - key: "connection"
      limit: 5
      window: 1m
    - key: "room_create"
      limit: 10
      window: 1h

features:
  message_retention_days: 365
  max_file_size: 10485760  # 10MB
  allowed_file_types: ["image/*", "application/pdf", ".doc", ".docx"]
  enable_read_receipts: true
  enable_typing_indicators: true
  enable_reactions: true
  enable_threads: true

observability:
  logging:
    level: "debug"  # debug, info, warn, error
    format: "json"  # json, console
    output: "stdout"  # stdout, file
    file_path: "logs/app.log"
    
  metrics:
    enabled: true
    port: 9090
    path: "/metrics"
    
  tracing:
    enabled: true
    exporter: "jaeger"  # jaeger, otlp
    jaeger:
      endpoint: "http://localhost:14268/api/traces"
    sampling_rate: 0.01  # 1%

storage:
  type: "minio"  # minio, s3
  minio:
    endpoint: "localhost:9000"
    access_key: "${MINIO_ACCESS_KEY}"
    secret_key: "${MINIO_SECRET_KEY}"
    bucket: "chat-uploads"
    use_ssl: false
  s3:
    region: "us-east-1"
    bucket: "chat-uploads"
```

---

## 9. Testing Strategy

### Unit Tests
```go
// Test message service
func TestMessageService_SendMessage(t *testing.T) {
    // Mock repository
    mockRepo := new(mockMessageRepo)
    svc := NewMessageService(mockRepo, ...)
    
    // Test cases
    tests := []struct {
        name      string
        input     SendMessageInput
        mockSetup func()
        want      *Message
        wantErr   bool
    }{...}
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            tt.mockSetup()
            got, err := svc.SendMessage(context.Background(), tt.input)
            // Assertions
        })
    }
}
```

### Integration Tests
- Database operations with testcontainers
- Redis pub/sub functionality
- WebSocket connection lifecycle
- Authentication flow

### Load Tests (k6)
```javascript
// load_test.js
import ws from 'k6/ws';
import { check, sleep } from 'k6';

export let options = {
  stages: [
    { duration: '2m', target: 100 },   // Ramp up
    { duration: '5m', target: 1000 },  // Stay at 1000
    { duration: '2m', target: 0 },     // Ramp down
  ],
  thresholds: {
    ws_connecting: ['p(95) < 100'],
    ws_msgs_sent: ['rate > 10'],
  },
};

export default function() {
  const url = 'wss://localhost:8080/ws';
  const token = getAuthToken();  // Get JWT
  
  const res = ws.connect(url + '?token=' + token, null, function(socket) {
    socket.on('open', () => {
      // Subscribe to room
      socket.send(JSON.stringify({
        type: 'subscribe',
        data: { room_ids: ['room-1'] }
      }));
    });
    
    socket.on('message', (msg) => {
      check(msg, { 'message received': (m) => m !== '' });
    });
    
    // Send messages periodically
    socket.setInterval(() => {
      socket.send(JSON.stringify({
        type: 'message',
        data: { room_id: 'room-1', content: 'Hello!' }
      }));
    }, 5000);
    
    socket.setTimeout(() => socket.close(), 60000);
  });
  
  check(res, { 'status is 101': (r) => r && r.status === 101 });
}
```

### E2E Tests
- Full user journey: register → login → create room → send messages → read receipts

---

## 10. Deployment

### Docker Compose (Local)
```yaml
version: '3.8'

services:
  server:
    build:
      context: .
      dockerfile: deployments/docker/Dockerfile.server
    ports:
      - "8080:8080"
      - "9090:9090"  # Metrics
    environment:
      - APP_ENVIRONMENT=development
      - DB_PASSWORD=postgres
      - REDIS_PASSWORD=
    volumes:
      - ./configs:/app/configs
    depends_on:
      - postgres
      - redis
      - minio
      
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_USER: chat
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: chat
    volumes:
      - postgres_data:/var/lib/postgresql/data
    ports:
      - "5432:5432"
      
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
      
  minio:
    image: minio/minio
    command: server /data --console-address ":9001"
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    ports:
      - "9000:9000"
      - "9001:9001"
    volumes:
      - minio_data:/data
      
  prometheus:
    image: prom/prometheus
    volumes:
      - ./deployments/prometheus:/etc/prometheus
    ports:
      - "9091:9090"
      
  grafana:
    image: grafana/grafana
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin

volumes:
  postgres_data:
  minio_data:
```

### Kubernetes Deployment
```yaml
# deployments/kubernetes/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: chat-server
  labels:
    app: chat-server
spec:
  replicas: 3
  selector:
    matchLabels:
      app: chat-server
  template:
    metadata:
      labels:
        app: chat-server
    spec:
      containers:
      - name: server
        image: chat-server:latest
        ports:
        - containerPort: 8080
          name: websocket
        - containerPort: 9090
          name: metrics
        env:
        - name: APP_ENVIRONMENT
          value: "production"
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: chat-secrets
              key: db-password
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
```

---

## 11. Security Checklist

- [ ] JWT with RS256 and proper key rotation
- [ ] Password hashing with bcrypt (cost 12+)
- [ ] Input validation on all endpoints
- [ ] XSS protection (sanitize user content)
- [ ] Rate limiting (per user, per IP)
- [ ] SQL injection prevention (parameterized queries)
- [ ] CORS configuration
- [ ] TLS 1.3 for all connections
- [ ] WebSocket origin validation
- [ ] File upload validation (type, size, scan)
- [ ] Secrets management (env vars, Vault)
- [ ] Security headers (HSTS, CSP, etc.)
- [ ] Audit logging for sensitive operations

---

## 12. Development Phases

### Phase 1: Core Foundation (Week 1)
- Project structure and boilerplate
- Database schema and migrations
- User authentication (JWT)
- Basic WebSocket server with connection management

### Phase 2: Real-time Messaging (Week 2)
- Message CRUD operations
- WebSocket protocol implementation
- Redis pub-sub integration
- Room subscription management

### Phase 3: Advanced Features (Week 3)
- Presence system
- Read receipts
- Message reactions
- Thread support
- File uploads

### Phase 4: Polish & Scale (Week 4)
- Comprehensive testing (unit, integration, load)
- Observability (metrics, logging, tracing)
- Rate limiting and security hardening
- Kubernetes deployment
- Documentation

---

## 13. Success Criteria

### Functional
- [ ] Users can register, login, manage profile
- [ ] Create rooms, invite members, manage permissions
- [ ] Send/receive real-time messages in rooms
- [ ] Message history with pagination
- [ ] Presence indicators (online/offline)
- [ ] Read receipts
- [ ] Reactions and threads
- [ ] File sharing

### Non-Functional
- [ ] Handle 10,000 concurrent connections per node
- [ ] < 50ms message latency (P99)
- [ ] Zero message loss during normal operation
- [ ] Graceful degradation under load
- [ ] 99.99% uptime target
- [ ] Complete observability (metrics, logs, traces)
- [ ] Production-ready deployment manifests

---

## Appendix A: WebSocket Message Examples

### Connection
```json
// Client
{"id": "1", "type": "ping"}

// Server
{"id": "srv-1", "type": "pong", "timestamp": "2024-01-15T10:30:00Z"}
```

### Send Message
```json
// Client
{
  "id": "msg-123",
  "type": "message",
  "timestamp": "2024-01-15T10:30:05Z",
  "data": {
    "room_id": "room-uuid",
    "content": "Hello everyone!",
    "client_id": "client-unique-id"
  }
}

// Server ACK
{
  "id": "srv-42",
  "type": "ack",
  "timestamp": "2024-01-15T10:30:05.050Z",
  "data": {
    "client_msg_id": "msg-123",
    "server_msg_id": "1634567890123456",
    "status": "delivered"
  }
}
```

### Error Response
```json
{
  "id": "srv-99",
  "type": "error",
  "timestamp": "2024-01-15T10:30:06Z",
  "data": {
    "client_msg_id": "msg-123",
    "code": "RATE_LIMITED",
    "message": "Too many messages. Please slow down.",
    "retry_after": 30
  }
}
```

---

## Appendix B: Database Indexes Strategy

### Critical Indexes
1. **Users**: `username` (unique), `email` (unique)
2. **Room Members**: Composite `(room_id, user_id)`, `(user_id)` with `left_at IS NULL`
3. **Messages**: Composite `(room_id, created_at DESC)` for history queries
4. **Messages**: `(parent_id)` for thread lookups
5. **Messages**: GIN index on `to_tsvector(content)` for search
6. **Read Receipts**: `(room_id, user_id)` composite primary key
7. **Files**: `(room_id)` for room file listings

### Partitioning Strategy
- **Messages**: Partition by `created_at` (monthly)
- **Old partitions**: Archive to S3 after retention period

---

## Appendix C: Monitoring & Alerting

### Key Metrics

#### Business Metrics
- `chat_messages_total` (counter by room_type)
- `chat_active_users` (gauge)
- `chat_rooms_total` (gauge by type)

#### Technical Metrics
- `websocket_connections_active` (gauge)
- `websocket_messages_sent_total` (counter)
- `websocket_messages_received_total` (counter)
- `websocket_connection_errors_total` (counter by error_type)
- `http_requests_duration_seconds` (histogram)
- `db_queries_duration_seconds` (histogram by query_type)
- `redis_operations_duration_seconds` (histogram by operation)
- `pubsub_publish_duration_seconds` (histogram)

#### Alerting Rules
- Connection count > 8000 per node
- Message latency P99 > 100ms
- Error rate > 1%
- DB connection pool > 80%
- Redis memory > 80%

---

**END OF SPECIFICATION**

This is a comprehensive guide. Build iteratively, test thoroughly, and prioritize reliability over features. The system should feel solid and production-ready.