package service

import (
	"context"
	"errors"
	"time"

	"github.com/websocket-chat/internal/cache"
	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/internal/pubsub"
	"github.com/websocket-chat/internal/repository"
)

type RoomService struct {
	roomRepo   repository.IRoomRepository
	userRepo   repository.IUserRepository
	redisCache cache.Cache
	ps         pubsub.PubSub
}

func NewRoomService(roomRepo repository.IRoomRepository, userRepo repository.IUserRepository, redisCache cache.Cache, ps pubsub.PubSub) *RoomService {
	return &RoomService{
		roomRepo:   roomRepo,
		userRepo:   userRepo,
		redisCache: redisCache,
		ps:         ps,
	}
}

type CreateRoomInput struct {
	Name        string
	Type        model.RoomType
	Description string
	CreatedBy   string
}

func (s *RoomService) Create(ctx context.Context, input CreateRoomInput) (*model.Room, error) {
	room := &model.Room{
		Name:        input.Name,
		Type:        input.Type,
		Description: input.Description,
		CreatedBy:   input.CreatedBy,
		Settings: model.RoomSettings{
			AllowReactions:    true,
			AllowThreads:      true,
			MessageRetention:  0,
			SlowModeSeconds:   0,
			RequireApproval:   false,
			OnlyAdminsCanPost: false,
		},
		MemberCount: 1,
	}

	owner := &model.RoomMember{
		RoomID:   "",
		UserID:   input.CreatedBy,
		Role:     model.RoleOwner,
		JoinedAt: time.Now(),
		Notifications: model.NotificationSettings{
			Enabled:      true,
			MentionsOnly: false,
			Sound:        true,
		},
	}

	if err := s.roomRepo.CreateRoomWithOwner(ctx, room, owner); err != nil {
		return nil, err
	}

	return room, nil
}

func (s *RoomService) GetByID(ctx context.Context, id string) (*model.Room, error) {
	cacheKey := "room:" + id
	var cached model.Room
	if err := s.redisCache.GetObject(ctx, cacheKey, &cached); err == nil {
		return &cached, nil
	}

	room, err := s.roomRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	s.redisCache.SetObject(ctx, cacheKey, room, 5*time.Minute)
	return room, nil
}

func (s *RoomService) GetUserRooms(ctx context.Context, userID string) ([]*model.Room, error) {
	return s.roomRepo.GetUserRooms(ctx, userID)
}

func (s *RoomService) Update(ctx context.Context, room *model.Room) error {
	err := s.roomRepo.Update(ctx, room)
	if err != nil {
		return err
	}

	cacheKey := "room:" + room.ID
	s.redisCache.Del(ctx, cacheKey)
	return nil
}

func (s *RoomService) Delete(ctx context.Context, id string) error {
	err := s.roomRepo.Delete(ctx, id)
	if err == nil {
		s.redisCache.Del(ctx, "room:"+id)
	}
	return err
}

func (s *RoomService) AddMember(ctx context.Context, roomID, userID string, role model.MemberRole) error {
	member := &model.RoomMember{
		RoomID:     roomID,
		UserID:     userID,
		Role:       role,
		JoinedAt:   time.Now(),
		LastReadAt: time.Now(),
		Notifications: model.NotificationSettings{
			Enabled:      true,
			MentionsOnly: false,
			Sound:        true,
		},
	}

	err := s.roomRepo.AddMember(ctx, member)
	if err != nil {
		return err
	}

	s.redisCache.Del(ctx, "room:"+roomID)
	return nil
}

func (s *RoomService) RemoveMember(ctx context.Context, roomID, userID string) error {
	err := s.roomRepo.RemoveMember(ctx, roomID, userID)
	if err != nil {
		return err
	}

	s.redisCache.Del(ctx, "room:"+roomID)
	return nil
}

func (s *RoomService) GetMembers(ctx context.Context, roomID string) ([]*model.RoomMember, error) {
	return s.roomRepo.GetMembers(ctx, roomID)
}

func (s *RoomService) IsMember(ctx context.Context, roomID, userID string) (bool, error) {
	return s.roomRepo.IsMember(ctx, roomID, userID)
}

func (s *RoomService) JoinRoom(ctx context.Context, roomID, userID string) error {
	if _, err := s.roomRepo.GetByID(ctx, roomID); err != nil {
		return errors.New("room not found")
	}

	member, err := s.roomRepo.GetMember(ctx, roomID, userID)
	if err == nil && member != nil {
		if member.BannedAt != nil {
			return errors.New("user is banned from this room")
		}
		if member.LeftAt == nil {
			return nil
		}
	}

	if member == nil {
		newMember := &model.RoomMember{
			RoomID:     roomID,
			UserID:     userID,
			Role:       model.RoleMember,
			JoinedAt:   time.Now(),
			LastReadAt: time.Now(),
			Notifications: model.NotificationSettings{
				Enabled:      true,
				MentionsOnly: false,
				Sound:        true,
			},
		}
		if err := s.roomRepo.JoinRoomTx(ctx, newMember); err != nil {
			return err
		}
		if s.redisCache != nil {
			s.redisCache.Del(ctx, "room:"+roomID)
		}
	}

	if s.ps != nil {
		s.ps.SubscribeToRoom(ctx, roomID, userID)
	}

	return nil
}

func (s *RoomService) LeaveRoom(ctx context.Context, roomID, userID string) error {
	if _, err := s.roomRepo.GetMember(ctx, roomID, userID); err != nil {
		return errors.New("not a member of this room")
	}

	if err := s.roomRepo.LeaveRoomTx(ctx, roomID, userID); err != nil {
		return err
	}
	if s.redisCache != nil {
		s.redisCache.Del(ctx, "room:"+roomID)
	}

	if s.ps != nil {
		s.ps.UnsubscribeFromRoom(ctx, roomID, userID)
	}

	return nil
}

func (s *RoomService) MarkRead(ctx context.Context, roomID, userID, messageID string) error {
	isMember, err := s.roomRepo.IsMember(ctx, roomID, userID)
	if err != nil {
		return err
	}
	if !isMember {
		return errors.New("not a member of this room")
	}
	return s.roomRepo.MarkRead(ctx, roomID, userID, messageID)
}
