package pubsub

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/websocket-chat/internal/model"
)

type PubSub interface {
	Publish(ctx context.Context, channel string, message []byte) error
	Subscribe(ctx context.Context, channels ...string) (Subscriber, error)
	PSubscribe(ctx context.Context, patterns ...string) (PatternSubscriber, error)
	SetPresence(ctx context.Context, userID string, presence *model.Presence) error
	GetPresence(ctx context.Context, userID string) (*model.Presence, error)
	GetPresences(ctx context.Context, userIDs []string) (map[string]*model.Presence, error)
	SubscribeToRoom(ctx context.Context, roomID, userID string) error
	UnsubscribeFromRoom(ctx context.Context, roomID, userID string) error
	GetRoomSubscribers(ctx context.Context, roomID string) ([]string, error)
	CheckRateLimit(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
	Close() error
}

type Subscriber interface {
	Channel() <-chan *redis.Message
	Close() error
}

type PatternSubscriber interface {
	Channel() <-chan *redis.Message
	Close() error
}

type RedisPubSub struct {
	client *redis.Client
	mu     sync.RWMutex
}

type Config struct {
	Addrs    []string
	Password string
	DB       int
	PoolSize int
}

func NewRedisClient(cfg *Config) *redis.Client {
	if cfg == nil {
		cfg = &Config{
			Addrs:    []string{"localhost:6379"},
			PoolSize: 10,
		}
	}

	addr := "localhost:6379"
	if len(cfg.Addrs) > 0 {
		addr = cfg.Addrs[0]
	}

	return redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: cfg.PoolSize,
	})
}

func NewRedisPubSub(client *redis.Client) *RedisPubSub {
	return &RedisPubSub{client: client}
}

func (p *RedisPubSub) Publish(ctx context.Context, channel string, message []byte) error {
	return p.client.Publish(ctx, channel, message).Err()
}

func (p *RedisPubSub) Subscribe(ctx context.Context, channels ...string) (Subscriber, error) {
	pubsub := p.client.Subscribe(ctx, channels...)
	return &RedisSubscriber{pubsub: pubsub}, nil
}

func (p *RedisPubSub) PSubscribe(ctx context.Context, patterns ...string) (PatternSubscriber, error) {
	pubsub := p.client.PSubscribe(ctx, patterns...)
	return &RedisPatternSubscriber{pubsub: pubsub}, nil
}

type RedisSubscriber struct {
	pubsub *redis.PubSub
}

func (s *RedisSubscriber) Channel() <-chan *redis.Message {
	return s.pubsub.Channel()
}

func (s *RedisSubscriber) Close() error {
	return s.pubsub.Close()
}

type RedisPatternSubscriber struct {
	pubsub *redis.PubSub
}

func (s *RedisPatternSubscriber) Channel() <-chan *redis.Message {
	return s.pubsub.Channel()
}

func (s *RedisPatternSubscriber) Close() error {
	return s.pubsub.Close()
}

func (p *RedisPubSub) SetPresence(ctx context.Context, userID string, presence *model.Presence) error {
	data, err := json.Marshal(presence)
	if err != nil {
		return err
	}
	key := "presence:" + userID
	if err := p.client.Set(ctx, key, data, 5*time.Minute).Err(); err != nil {
		return err
	}

	broadcastData, _ := json.Marshal(map[string]interface{}{
		"user_id":   userID,
		"presence":  presence,
		"timestamp": time.Now().Unix(),
	})
	return p.Publish(ctx, "ws:presence", broadcastData)
}

func (p *RedisPubSub) GetPresence(ctx context.Context, userID string) (*model.Presence, error) {
	key := "presence:" + userID
	data, err := p.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	var presence model.Presence
	if err := json.Unmarshal(data, &presence); err != nil {
		return nil, err
	}
	return &presence, nil
}

func (p *RedisPubSub) GetPresences(ctx context.Context, userIDs []string) (map[string]*model.Presence, error) {
	result := make(map[string]*model.Presence)
	for _, userID := range userIDs {
		presence, err := p.GetPresence(ctx, userID)
		if err != nil {
			continue
		}
		if presence != nil {
			result[userID] = presence
		}
	}
	return result, nil
}

func (p *RedisPubSub) SubscribeToRoom(ctx context.Context, roomID, userID string) error {
	key := "room:" + roomID + ":subscribers"
	if err := p.client.SAdd(ctx, key, userID).Err(); err != nil {
		return err
	}

	joinData, _ := json.Marshal(map[string]interface{}{
		"room_id":   roomID,
		"user_id":   userID,
		"action":    "joined",
		"timestamp": time.Now().Unix(),
	})
	return p.Publish(ctx, "ws:room:"+roomID+":events", joinData)
}

func (p *RedisPubSub) UnsubscribeFromRoom(ctx context.Context, roomID, userID string) error {
	key := "room:" + roomID + ":subscribers"
	if err := p.client.SRem(ctx, key, userID).Err(); err != nil {
		return err
	}

	leaveData, _ := json.Marshal(map[string]interface{}{
		"room_id":   roomID,
		"user_id":   userID,
		"action":    "left",
		"timestamp": time.Now().Unix(),
	})
	return p.Publish(ctx, "ws:room:"+roomID+":events", leaveData)
}

func (p *RedisPubSub) GetRoomSubscribers(ctx context.Context, roomID string) ([]string, error) {
	key := "room:" + roomID + ":subscribers"
	return p.client.SMembers(ctx, key).Result()
}

func (p *RedisPubSub) CheckRateLimit(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	rateKey := "ratelimit:" + key
	now := time.Now().UnixNano()

	pipe := p.client.Pipeline()
	pipe.ZRemRangeByScore(ctx, rateKey, "0", strconv.FormatInt(now-int64(window), 10))
	pipe.ZAdd(ctx, rateKey, redis.Z{Score: float64(now), Member: now})
	pipe.ZCard(ctx, rateKey)
	pipe.Expire(ctx, rateKey, window)

	results, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}

	count := results[2].(*redis.IntCmd).Val()
	return int(count) <= limit, nil
}

func (p *RedisPubSub) Close() error {
	return p.client.Close()
}
