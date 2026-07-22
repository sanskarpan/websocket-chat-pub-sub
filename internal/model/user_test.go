package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUserStatus(t *testing.T) {
	assert.Equal(t, UserStatus("online"), StatusOnline)
	assert.Equal(t, UserStatus("away"), StatusAway)
	assert.Equal(t, UserStatus("dnd"), StatusDND)
	assert.Equal(t, UserStatus("offline"), StatusOffline)
}

func TestRoomType(t *testing.T) {
	assert.Equal(t, RoomType("direct"), RoomTypeDirect)
	assert.Equal(t, RoomType("group"), RoomTypeGroup)
	assert.Equal(t, RoomType("channel"), RoomTypeChannel)
}

func TestMemberRole(t *testing.T) {
	assert.Equal(t, MemberRole("owner"), RoleOwner)
	assert.Equal(t, MemberRole("admin"), RoleAdmin)
	assert.Equal(t, MemberRole("moderator"), RoleModerator)
	assert.Equal(t, MemberRole("member"), RoleMember)
}

func TestContentType(t *testing.T) {
	assert.Equal(t, ContentType("text"), ContentTypeText)
	assert.Equal(t, ContentType("markdown"), ContentTypeMarkdown)
	assert.Equal(t, ContentType("system"), ContentTypeSystem)
	assert.Equal(t, ContentType("file"), ContentTypeFile)
}

func TestUserCreation(t *testing.T) {
	user := User{
		ID:          "test-id",
		Username:    "testuser",
		Email:       "test@example.com",
		DisplayName: "Test User",
		Status:      StatusOnline,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	assert.NotEmpty(t, user.ID)
	assert.Equal(t, "testuser", user.Username)
	assert.Equal(t, StatusOnline, user.Status)
}

func TestRoomCreation(t *testing.T) {
	room := Room{
		ID:          "room-id",
		Name:        "general",
		Type:        RoomTypeChannel,
		CreatedBy:   "user-id",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		MemberCount: 1,
		Settings: RoomSettings{
			AllowReactions: true,
			AllowThreads:   true,
		},
	}

	assert.Equal(t, "general", room.Name)
	assert.Equal(t, RoomTypeChannel, room.Type)
	assert.True(t, room.Settings.AllowReactions)
}

func TestMessageCreation(t *testing.T) {
	msg := Message{
		ID:          "msg-id",
		RoomID:      "room-id",
		UserID:      "user-id",
		Content:     "Hello!",
		ContentType: ContentTypeText,
		CreatedAt:   time.Now(),
		Reactions:   make(map[string][]string),
	}

	assert.Equal(t, "Hello!", msg.Content)
	assert.Equal(t, ContentTypeText, msg.ContentType)
	assert.NotNil(t, msg.Reactions)
}

func TestPresenceCreation(t *testing.T) {
	presence := Presence{
		UserID:     "user-id",
		Status:     StatusOnline,
		LastActive: time.Now(),
		ClientInfo: ClientInfo{
			Platform: "web",
			Version:  "1.0.0",
		},
	}

	assert.Equal(t, StatusOnline, presence.Status)
	assert.Equal(t, "web", presence.ClientInfo.Platform)
}
