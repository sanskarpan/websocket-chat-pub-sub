package service

import (
	"context"
	"time"

	"github.com/websocket-chat/internal/cache"
	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/internal/repository"
)

type UserService struct {
	userRepo   *repository.UserRepository
	redisCache cache.Cache
}

func NewUserService(userRepo *repository.UserRepository, redisCache cache.Cache) *UserService {
	return &UserService{
		userRepo:   userRepo,
		redisCache: redisCache,
	}
}

func (s *UserService) GetByID(ctx context.Context, id string) (*model.User, error) {
	cacheKey := "user:" + id
	var cached model.User
	if err := s.redisCache.GetObject(ctx, cacheKey, &cached); err == nil {
		return &cached, nil
	}

	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	s.redisCache.SetObject(ctx, cacheKey, user, 5*time.Minute)
	return user, nil
}

func (s *UserService) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	return s.userRepo.GetByUsername(ctx, username)
}

func (s *UserService) Update(ctx context.Context, user *model.User) error {
	err := s.userRepo.Update(ctx, user)
	if err != nil {
		return err
	}

	cacheKey := "user:" + user.ID
	s.redisCache.Del(ctx, cacheKey)
	return nil
}

func (s *UserService) Search(ctx context.Context, query string, limit int) ([]*model.User, error) {
	return s.userRepo.Search(ctx, query, limit)
}

func (s *UserService) UpdateStatus(ctx context.Context, userID string, status model.UserStatus) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	now := time.Now()
	user.Status = status
	if status == model.StatusOffline {
		user.LastSeenAt = &now
	}

	return s.userRepo.Update(ctx, user)
}
