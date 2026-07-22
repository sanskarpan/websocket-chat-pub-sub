package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/internal/pubsub"
)

type PresenceService struct {
	pubsub pubsub.PubSub
}

func NewPresenceService(ps pubsub.PubSub) *PresenceService {
	return &PresenceService{pubsub: ps}
}

func (s *PresenceService) SetPresence(ctx context.Context, userID string, status model.UserStatus, clientInfo model.ClientInfo) error {
	presence := &model.Presence{
		UserID:     userID,
		Status:     status,
		LastActive: time.Now(),
		ClientInfo: clientInfo,
	}

	if s.pubsub != nil {
		if err := s.pubsub.SetPresence(ctx, userID, presence); err != nil {
			return err
		}
	}
	return nil
}

func (s *PresenceService) GetPresence(ctx context.Context, userID string) (*model.Presence, error) {
	if s.pubsub != nil {
		return s.pubsub.GetPresence(ctx, userID)
	}
	return nil, nil
}

func (s *PresenceService) GetPresences(ctx context.Context, userIDs []string) (map[string]*model.Presence, error) {
	if s.pubsub != nil {
		return s.pubsub.GetPresences(ctx, userIDs)
	}
	return make(map[string]*model.Presence), nil
}

func (s *PresenceService) BroadcastPresence(ctx context.Context, presence *model.Presence) error {
	if s.pubsub == nil {
		return nil
	}

	data, err := json.Marshal(map[string]interface{}{
		"user_id":   presence.UserID,
		"status":    presence.Status,
		"presence":  presence,
		"timestamp": time.Now().Unix(),
	})
	if err != nil {
		return err
	}

	return s.pubsub.Publish(ctx, "ws:presence", data)
}

func (s *PresenceService) SetAndBroadcast(ctx context.Context, userID string, status model.UserStatus, clientInfo model.ClientInfo) error {
	presence := &model.Presence{
		UserID:     userID,
		Status:     status,
		LastActive: time.Now(),
		ClientInfo: clientInfo,
	}

	if s.pubsub != nil {
		if err := s.pubsub.SetPresence(ctx, userID, presence); err != nil {
			return err
		}

		data, err := json.Marshal(map[string]interface{}{
			"user_id":   userID,
			"status":    status,
			"presence":  presence,
			"timestamp": time.Now().Unix(),
		})
		if err != nil {
			return err
		}

		return s.pubsub.Publish(ctx, "ws:presence", data)
	}
	return nil
}
