package model

import (
	"time"
)

type UserStatus string

const (
	StatusOnline  UserStatus = "online"
	StatusAway    UserStatus = "away"
	StatusDND     UserStatus = "dnd"
	StatusOffline UserStatus = "offline"
)

type User struct {
	ID           string     `json:"id" db:"id"`
	Username     string     `json:"username" db:"username"`
	Email        string     `json:"email" db:"email"`
	PasswordHash string     `json:"-" db:"password_hash"`
	DisplayName  string     `json:"display_name" db:"display_name"`
	AvatarURL    string     `json:"avatar_url" db:"avatar_url"`
	Status       UserStatus `json:"status" db:"status"`
	LastSeenAt   *time.Time `json:"last_seen_at" db:"last_seen_at"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
	Metadata     []byte     `json:"metadata" db:"metadata"`
}

type RoomType string

const (
	RoomTypeDirect  RoomType = "direct"
	RoomTypeGroup   RoomType = "group"
	RoomTypeChannel RoomType = "channel"
)

type RoomSettings struct {
	AllowReactions    bool `json:"allow_reactions"`
	AllowThreads      bool `json:"allow_threads"`
	MessageRetention  int  `json:"message_retention_days"`
	SlowModeSeconds   int  `json:"slow_mode_seconds"`
	RequireApproval   bool `json:"require_approval"`
	OnlyAdminsCanPost bool `json:"only_admins_can_post"`
}

type Room struct {
	ID          string       `json:"id" db:"id"`
	Name        string       `json:"name" db:"name"`
	Type        RoomType     `json:"type" db:"type"`
	Description string       `json:"description" db:"description"`
	AvatarURL   string       `json:"avatar_url" db:"avatar_url"`
	CreatedBy   string       `json:"created_by" db:"created_by"`
	CreatedAt   time.Time    `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at" db:"updated_at"`
	ArchivedAt  *time.Time   `json:"archived_at,omitempty" db:"archived_at"`
	Settings    RoomSettings `json:"settings" db:"settings"`
	MemberCount int          `json:"member_count" db:"member_count"`
}

type MemberRole string

const (
	RoleOwner     MemberRole = "owner"
	RoleAdmin     MemberRole = "admin"
	RoleModerator MemberRole = "moderator"
	RoleMember    MemberRole = "member"
)

type NotificationSettings struct {
	Enabled      bool `json:"enabled"`
	MentionsOnly bool `json:"mentions_only"`
	Sound        bool `json:"sound"`
}

type RoomMember struct {
	RoomID        string               `json:"room_id" db:"room_id"`
	UserID        string               `json:"user_id" db:"user_id"`
	Role          MemberRole           `json:"role" db:"role"`
	JoinedAt      time.Time            `json:"joined_at" db:"joined_at"`
	LeftAt        *time.Time           `json:"left_at,omitempty" db:"left_at"`
	LastReadAt    time.Time            `json:"last_read_at" db:"last_read_at"`
	MutedUntil    *time.Time           `json:"muted_until,omitempty" db:"muted_until"`
	BannedAt      *time.Time           `json:"banned_at,omitempty" db:"banned_at"`
	BanReason     *string              `json:"ban_reason,omitempty" db:"ban_reason"`
	Notifications NotificationSettings `json:"notifications" db:"notifications"`
}

type ContentType string

const (
	ContentTypeText     ContentType = "text"
	ContentTypeMarkdown ContentType = "markdown"
	ContentTypeSystem   ContentType = "system"
	ContentTypeFile     ContentType = "file"
)

type Attachment struct {
	ID           string `json:"id"`
	FileName     string `json:"file_name"`
	FileSize     int64  `json:"file_size"`
	MimeType     string `json:"mime_type"`
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
}

type ForwardedInfo struct {
	RoomID    string    `json:"room_id"`
	MessageID string    `json:"message_id"`
	UserID    string    `json:"user_id"`
	At        time.Time `json:"at"`
}

type MessageMetadata struct {
	ClientID      string                 `json:"client_id,omitempty"`
	ReplyTo       *string                `json:"reply_to,omitempty"`
	ForwardedFrom *ForwardedInfo         `json:"forwarded_from,omitempty"`
	CustomData    map[string]interface{} `json:"custom_data,omitempty"`
}

type Message struct {
	ID              string              `json:"id" db:"id"`
	RoomID          string              `json:"room_id" db:"room_id"`
	UserID          string              `json:"user_id" db:"user_id"`
	Content         string              `json:"content" db:"content"`
	ContentType     ContentType         `json:"content_type" db:"content_type"`
	ParentID        *string             `json:"parent_id,omitempty" db:"parent_id"`
	ThreadCount     int                 `json:"thread_count" db:"thread_count"`
	EditedAt        *time.Time          `json:"edited_at,omitempty" db:"edited_at"`
	DeletedAt       *time.Time          `json:"deleted_at,omitempty" db:"deleted_at"`
	DeletedBy       *string             `json:"deleted_by,omitempty" db:"deleted_by"`
	Reactions       map[string][]string `json:"reactions" db:"reactions"`
	Attachments     []Attachment        `json:"attachments" db:"attachments"`
	Metadata        MessageMetadata     `json:"metadata" db:"metadata"`
	CreatedAt       time.Time           `json:"created_at" db:"created_at"`
	ClientTimestamp *time.Time          `json:"client_timestamp,omitempty" db:"client_timestamp"`
}

type Presence struct {
	UserID      string     `json:"user_id"`
	Status      UserStatus `json:"status"`
	LastActive  time.Time  `json:"last_active"`
	CurrentRoom *string    `json:"current_room,omitempty"`
	ClientInfo  ClientInfo `json:"client_info"`
}

type ClientInfo struct {
	Platform string `json:"platform"`
	Version  string `json:"version"`
	DeviceID string `json:"device_id"`
}

type ReadReceipt struct {
	RoomID    string    `json:"room_id" db:"room_id"`
	UserID    string    `json:"user_id" db:"user_id"`
	MessageID string    `json:"message_id" db:"message_id"`
	ReadAt    time.Time `json:"read_at" db:"read_at"`
}
