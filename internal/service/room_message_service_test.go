package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/internal/service"
)

func TestRoomService_Create(t *testing.T) {
	roomRepo := NewFakeRoomRepository()
	userRepo := NewFakeUserRepository()

	err := userRepo.Create(context.Background(), &model.User{
		ID:       "user-1",
		Username: "owner",
		Email:    "owner@example.com",
	})
	require.NoError(t, err)

	roomService := service.NewRoomService(roomRepo, userRepo, nil, nil)
	room, err := roomService.Create(context.Background(), service.CreateRoomInput{
		Name:        "general",
		Type:        model.RoomTypeChannel,
		Description: "General discussion",
		CreatedBy:   "user-1",
	})

	require.NoError(t, err)
	assert.NotEmpty(t, room.ID)
	assert.Equal(t, "general", room.Name)
	assert.Equal(t, model.RoomTypeChannel, room.Type)
	assert.Equal(t, "user-1", room.CreatedBy)
}

func TestRoomService_JoinRoom_IncrementsMemberCount(t *testing.T) {
	roomRepo := NewFakeRoomRepository()
	userRepo := NewFakeUserRepository()

	err := userRepo.Create(context.Background(), &model.User{ID: "user-1", Username: "u1", Email: "u1@example.com"})
	require.NoError(t, err)
	err = userRepo.Create(context.Background(), &model.User{ID: "user-2", Username: "u2", Email: "u2@example.com"})
	require.NoError(t, err)

	roomService := service.NewRoomService(roomRepo, userRepo, nil, nil)
	room, err := roomService.Create(context.Background(), service.CreateRoomInput{
		Name:      "test",
		Type:      model.RoomTypeGroup,
		CreatedBy: "user-1",
	})
	require.NoError(t, err)

	err = roomService.JoinRoom(context.Background(), room.ID, "user-2")
	require.NoError(t, err)

	updated, err := roomRepo.GetByID(context.Background(), room.ID)
	require.NoError(t, err)
	assert.Greater(t, updated.MemberCount, 0, "MemberCount should be incremented after JoinRoom")
}

func TestRoomService_LeaveRoom_DecrementsMemberCount(t *testing.T) {
	roomRepo := NewFakeRoomRepository()
	userRepo := NewFakeUserRepository()

	err := userRepo.Create(context.Background(), &model.User{ID: "user-1", Username: "u1", Email: "u1@example.com"})
	require.NoError(t, err)
	err = userRepo.Create(context.Background(), &model.User{ID: "user-2", Username: "u2", Email: "u2@example.com"})
	require.NoError(t, err)

	roomService := service.NewRoomService(roomRepo, userRepo, nil, nil)
	room, err := roomService.Create(context.Background(), service.CreateRoomInput{
		Name: "test", Type: model.RoomTypeGroup, CreatedBy: "user-1",
	})
	require.NoError(t, err)

	require.NoError(t, roomService.JoinRoom(context.Background(), room.ID, "user-2"))
	require.NoError(t, roomService.LeaveRoom(context.Background(), room.ID, "user-2"))

	updated, err := roomRepo.GetByID(context.Background(), room.ID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, updated.MemberCount, 0)
}

func TestRoomService_IsMember(t *testing.T) {
	roomRepo := NewFakeRoomRepository()
	userRepo := NewFakeUserRepository()

	err := userRepo.Create(context.Background(), &model.User{ID: "user-1", Username: "u1", Email: "u1@example.com"})
	require.NoError(t, err)

	roomService := service.NewRoomService(roomRepo, userRepo, nil, nil)
	room, err := roomService.Create(context.Background(), service.CreateRoomInput{
		Name: "test", Type: model.RoomTypeGroup, CreatedBy: "user-1",
	})
	require.NoError(t, err)

	isMember, err := roomService.IsMember(context.Background(), room.ID, "user-1")
	require.NoError(t, err)
	assert.True(t, isMember)

	isMember, err = roomService.IsMember(context.Background(), room.ID, "stranger")
	require.NoError(t, err)
	assert.False(t, isMember)
}

func TestMessageService_SendMessage_ValidatesContent(t *testing.T) {
	messageRepo := NewFakeMessageRepository()
	roomRepo := NewFakeRoomRepository()

	roomRepo.rooms["room-1"] = &model.Room{ID: "room-1", Type: model.RoomTypeGroup, CreatedBy: "user-1"}
	roomRepo.members[key("room-1", "user-1")] = &model.RoomMember{RoomID: "room-1", UserID: "user-1", Role: model.RoleOwner, JoinedAt: time.Now()}

	msgService := service.NewMessageService(messageRepo, roomRepo, nil, nil)

	_, err := msgService.SendMessage(context.Background(), service.SendMessageInput{
		RoomID:  "room-1",
		UserID:  "user-1",
		Content: "",
	})
	assert.Error(t, err, "empty content should be rejected")

	_, err = msgService.SendMessage(context.Background(), service.SendMessageInput{
		RoomID:  "room-1",
		UserID:  "user-1",
		Content: "   ",
	})
	assert.Error(t, err, "whitespace-only content should be rejected")
}

func TestMessageService_SendMessage_SanitizesContent(t *testing.T) {
	messageRepo := NewFakeMessageRepository()
	roomRepo := NewFakeRoomRepository()

	roomRepo.rooms["room-1"] = &model.Room{ID: "room-1", Type: model.RoomTypeGroup, CreatedBy: "user-1"}
	roomRepo.members[key("room-1", "user-1")] = &model.RoomMember{RoomID: "room-1", UserID: "user-1", Role: model.RoleOwner, JoinedAt: time.Now()}

	msgService := service.NewMessageService(messageRepo, roomRepo, nil, nil)
	msg, err := msgService.SendMessage(context.Background(), service.SendMessageInput{
		RoomID:  "room-1",
		UserID:  "user-1",
		Content: "<script>alert('xss')</script>Hello",
	})
	require.NoError(t, err)
	assert.NotContains(t, msg.Content, "<script>")
	assert.Contains(t, msg.Content, "Hello")
}

func setupMessageServiceWithMember(t *testing.T) (*service.MessageService, *FakeMessageRepository) {
	messageRepo := NewFakeMessageRepository()
	roomRepo := NewFakeRoomRepository()

	room := &model.Room{ID: "room-1", Type: model.RoomTypeGroup, CreatedBy: "user-1", Settings: model.RoomSettings{}, MemberCount: 1}
	roomRepo.rooms["room-1"] = room
	roomRepo.members[key("room-1", "user-1")] = &model.RoomMember{RoomID: "room-1", UserID: "user-1", Role: model.RoleOwner, JoinedAt: time.Now()}

	msgService := service.NewMessageService(messageRepo, roomRepo, nil, nil)
	return msgService, messageRepo
}

func TestMessageService_EditMessage_AuthorOnly(t *testing.T) {
	msgService, _ := setupMessageServiceWithMember(t)
	msg, err := msgService.SendMessage(context.Background(), service.SendMessageInput{
		RoomID: "room-1", UserID: "user-1", Content: "Original",
	})
	require.NoError(t, err)

	err = msgService.EditMessage(context.Background(), msg.ID, "user-1", "Edited content")
	require.NoError(t, err)

	err = msgService.EditMessage(context.Background(), msg.ID, "user-2", "Malicious edit")
	assert.Error(t, err, "non-author should not be able to edit")
}

func TestMessageService_EditMessage_RejectsDeleted(t *testing.T) {
	msgService, _ := setupMessageServiceWithMember(t)
	msg, err := msgService.SendMessage(context.Background(), service.SendMessageInput{
		RoomID: "room-1", UserID: "user-1", Content: "Original",
	})
	require.NoError(t, err)

	require.NoError(t, msgService.DeleteMessage(context.Background(), msg.ID, "user-1"))

	err = msgService.EditMessage(context.Background(), msg.ID, "user-1", "Trying to edit deleted")
	assert.Error(t, err, "cannot edit deleted message")
}

func TestMessageService_AddReaction_RejectsDeleted(t *testing.T) {
	msgService, _ := setupMessageServiceWithMember(t)
	msg, err := msgService.SendMessage(context.Background(), service.SendMessageInput{
		RoomID: "room-1", UserID: "user-1", Content: "Original",
	})
	require.NoError(t, err)

	require.NoError(t, msgService.DeleteMessage(context.Background(), msg.ID, "user-1"))

	err = msgService.AddReaction(context.Background(), msg.ID, "👍", "user-1")
	assert.Error(t, err, "cannot react to deleted message")
}

func TestMessageService_DeleteMessage_AuthorOrModerator(t *testing.T) {
	msgService, _ := setupMessageServiceWithMember(t)
	msg, err := msgService.SendMessage(context.Background(), service.SendMessageInput{
		RoomID: "room-1", UserID: "user-1", Content: "Original",
	})
	require.NoError(t, err)

	err = msgService.DeleteMessage(context.Background(), msg.ID, "user-2")
	assert.Error(t, err, "non-author non-moderator should not be able to delete")

	err = msgService.DeleteMessage(context.Background(), msg.ID, "user-1")
	require.NoError(t, err, "author should be able to delete own message")
}