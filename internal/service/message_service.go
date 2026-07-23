package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/websocket-chat/internal/config"
	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/internal/pubsub"
	"github.com/websocket-chat/internal/repository"
	"github.com/websocket-chat/pkg/sanitization"
	"github.com/websocket-chat/pkg/validator"
)

type MessageService struct {
	messageRepo repository.IMessageRepository
	roomRepo    repository.IRoomRepository
	pubsub      pubsub.PubSub
	cfg         *config.Config
}

func NewMessageService(messageRepo repository.IMessageRepository, roomRepo repository.IRoomRepository, ps pubsub.PubSub, cfg *config.Config) *MessageService {
	return &MessageService{
		messageRepo: messageRepo,
		roomRepo:    roomRepo,
		pubsub:      ps,
		cfg:         cfg,
	}
}

type SendMessageInput struct {
	RoomID      string
	UserID      string
	Content     string
	ContentType model.ContentType
	ParentID    *string
	ClientID    string
}

func (s *MessageService) SendMessage(ctx context.Context, input SendMessageInput) (*model.Message, error) {
	content := sanitization.SanitizeMessage(input.Content)
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errors.New("content cannot be empty")
	}
	if err := validator.ValidateMessageContent(content); err != nil {
		return nil, err
	}

	msg := &model.Message{
		RoomID:      input.RoomID,
		UserID:      input.UserID,
		Content:     content,
		ContentType: input.ContentType,
		ParentID:    input.ParentID,
	}

	if err := s.messageRepo.Create(ctx, msg); err != nil {
		return nil, err
	}

	if s.pubsub != nil {
		data, _ := json.Marshal(map[string]interface{}{
			"room_id": msg.RoomID,
			"message": msg,
		})
		s.pubsub.Publish(ctx, "ws:room:"+msg.RoomID, data)
	}

	return msg, nil
}

func (s *MessageService) GetMessage(ctx context.Context, id string) (*model.Message, error) {
	return s.messageRepo.GetByID(ctx, id)
}

func (s *MessageService) GetRoomMessages(ctx context.Context, roomID string, limit int, before *time.Time) ([]*model.Message, error) {
	return s.messageRepo.GetByRoom(ctx, roomID, limit, before)
}

func (s *MessageService) EditMessage(ctx context.Context, msgID, requesterID, content string) error {
	content = sanitization.SanitizeMessage(content)
	content = strings.TrimSpace(content)
	if content == "" {
		return errors.New("content cannot be empty")
	}
	if err := validator.ValidateMessageContent(content); err != nil {
		return err
	}

	msg, err := s.messageRepo.GetByID(ctx, msgID)
	if err != nil {
		return err
	}

	if msg.DeletedAt != nil {
		return errors.New("cannot edit deleted message")
	}

	if msg.UserID != requesterID {
		return errors.New("unauthorized: only message author can edit this message")
	}

	isMember, err := s.roomRepo.IsMember(ctx, msg.RoomID, requesterID)
	if err != nil || !isMember {
		return errors.New("unauthorized: not a member of this room")
	}

	msg.Content = content
	if err := s.messageRepo.Update(ctx, msg); err != nil {
		return err
	}

	if s.pubsub != nil {
		data, _ := json.Marshal(map[string]interface{}{
			"room_id": msg.RoomID,
			"message": msg,
			"action":  "edited",
		})
		s.pubsub.Publish(ctx, "ws:room:"+msg.RoomID, data)
	}

	return nil
}

func (s *MessageService) DeleteMessage(ctx context.Context, msgID, requesterID string) error {
	msg, err := s.messageRepo.GetByID(ctx, msgID)
	if err != nil {
		return err
	}

	if msg.DeletedAt != nil {
		return errors.New("message already deleted")
	}

	if msg.UserID != requesterID {
		member, err := s.roomRepo.GetMember(ctx, msg.RoomID, requesterID)
		if err != nil || member == nil {
			return errors.New("unauthorized: permission denied to delete this message")
		}
		if member.Role != model.RoleOwner && member.Role != model.RoleAdmin && member.Role != model.RoleModerator {
			return errors.New("unauthorized: only author or room moderator/admin can delete this message")
		}
	}

	isMember, err := s.roomRepo.IsMember(ctx, msg.RoomID, requesterID)
	if err != nil || !isMember {
		return errors.New("unauthorized: not a member of this room")
	}

	if err := s.messageRepo.Delete(ctx, msgID, requesterID); err != nil {
		return err
	}

	if s.pubsub != nil {
		data, _ := json.Marshal(map[string]interface{}{
			"room_id":    msg.RoomID,
			"message_id": msgID,
			"action":     "deleted",
		})
		s.pubsub.Publish(ctx, "ws:room:"+msg.RoomID, data)
	}

	return nil
}

func (s *MessageService) GetThread(ctx context.Context, parentID string, limit int) ([]*model.Message, error) {
	return s.messageRepo.GetThread(ctx, parentID, limit)
}

func (s *MessageService) AddReaction(ctx context.Context, msgID, emoji, userID string) error {
	msg, err := s.messageRepo.GetByID(ctx, msgID)
	if err != nil {
		return err
	}

	if msg.DeletedAt != nil {
		return errors.New("cannot react to deleted message")
	}

	if msg.Reactions == nil {
		msg.Reactions = make(map[string][]string)
	}

	for _, u := range msg.Reactions[emoji] {
		if u == userID {
			return nil
		}
	}

	msg.Reactions[emoji] = append(msg.Reactions[emoji], userID)
	if err := s.messageRepo.UpdateReactionsTx(ctx, msg.ID, msg.Reactions); err != nil {
		return err
	}

	if s.pubsub != nil {
		data, _ := json.Marshal(map[string]interface{}{
			"room_id":    msg.RoomID,
			"message_id": msgID,
			"emoji":      emoji,
			"user_id":    userID,
			"action":     "added",
		})
		s.pubsub.Publish(ctx, "ws:room:"+msg.RoomID, data)
	}

	return nil
}

func (s *MessageService) RemoveReaction(ctx context.Context, msgID, emoji, userID string) error {
	msg, err := s.messageRepo.GetByID(ctx, msgID)
	if err != nil {
		return err
	}

	if msg.DeletedAt != nil {
		return errors.New("cannot remove reaction from deleted message")
	}

	if msg.Reactions != nil {
		users := msg.Reactions[emoji]
		found := false
		for i, u := range users {
			if u == userID {
				msg.Reactions[emoji] = append(users[:i], users[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			return nil
		}
		if err := s.messageRepo.UpdateReactionsTx(ctx, msg.ID, msg.Reactions); err != nil {
			return err
		}

		if s.pubsub != nil {
			data, _ := json.Marshal(map[string]interface{}{
				"room_id":    msg.RoomID,
				"message_id": msgID,
				"emoji":      emoji,
				"user_id":    userID,
				"action":     "removed",
			})
			s.pubsub.Publish(ctx, "ws:room:"+msg.RoomID, data)
		}
	}

	return nil
}
