package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/websocket-chat/internal/config"
	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/internal/pubsub"
	"github.com/websocket-chat/internal/repository"
)

type MessageService struct {
	messageRepo *repository.MessageRepository
	roomRepo    *repository.RoomRepository
	pubsub      pubsub.PubSub
	cfg         *config.Config
}

func NewMessageService(messageRepo *repository.MessageRepository, roomRepo *repository.RoomRepository, ps pubsub.PubSub, cfg *config.Config) *MessageService {
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
	msg := &model.Message{
		RoomID:      input.RoomID,
		UserID:      input.UserID,
		Content:     input.Content,
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
	msg, err := s.messageRepo.GetByID(ctx, msgID)
	if err != nil {
		return err
	}

	if msg.UserID != requesterID {
		return errors.New("unauthorized: only message author can edit this message")
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

	if msg.UserID != requesterID {
		isMember, err := s.roomRepo.IsMember(ctx, msg.RoomID, requesterID)
		if err != nil || !isMember {
			return errors.New("unauthorized: permission denied to delete this message")
		}
		members, err := s.roomRepo.GetMembers(ctx, msg.RoomID)
		if err != nil {
			return errors.New("unauthorized: permission denied")
		}
		authorized := false
		for _, m := range members {
			if m.UserID == requesterID && (m.Role == model.RoleOwner || m.Role == model.RoleAdmin || m.Role == model.RoleModerator) {
				authorized = true
				break
			}
		}
		if !authorized {
			return errors.New("unauthorized: only author or room moderator/admin can delete this message")
		}
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

	if msg.Reactions == nil {
		msg.Reactions = make(map[string][]string)
	}

	msg.Reactions[emoji] = append(msg.Reactions[emoji], userID)
	if err := s.messageRepo.Update(ctx, msg); err != nil {
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

	if msg.Reactions != nil {
		users := msg.Reactions[emoji]
		for i, u := range users {
			if u == userID {
				msg.Reactions[emoji] = append(users[:i], users[i+1:]...)
				break
			}
		}
		if err := s.messageRepo.Update(ctx, msg); err != nil {
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
