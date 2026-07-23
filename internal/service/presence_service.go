package service

import (
	"context"
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
	if s.pubsub == nil {
		return nil
	}

	presence := &model.Presence{
		UserID:     userID,
		Status:     status,
		LastActive: time.Now(),
		ClientInfo: clientInfo,
	}

	return s.pubsub.SetPresence(ctx, userID, presence)
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

func (s *PresenceService) SetAndBroadcast(ctx context.Context, userID string, status model.UserStatus, clientInfo model.ClientInfo) error {
	return s.SetPresence(ctx, userID, status, clientInfo)
}
