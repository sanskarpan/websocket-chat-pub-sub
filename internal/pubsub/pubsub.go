package pubsub

import (
	"context"
	"encoding/json"
	"strconv"
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
	InvalidateToken(ctx context.Context, jti string, ttl time.Duration) error
	IsTokenInvalidated(ctx context.Context, jti string) (bool, error)
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
	if len(userIDs) == 0 {
		return make(map[string]*model.Presence), nil
	}

	keys := make([]string, len(userIDs))
	for i, userID := range userIDs {
		keys[i] = "presence:" + userID
	}

	values, err := p.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	result := make(map[string]*model.Presence)
	for i, val := range values {
		if val == nil {
			continue
		}
		strVal, ok := val.(string)
		if !ok {
			continue
		}
		var presence model.Presence
		if err := json.Unmarshal([]byte(strVal), &presence); err != nil {
			continue
		}
		result[userIDs[i]] = &presence
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

var rateLimitLuaScript = redis.NewScript(`
	local key = KEYS[1]
	local now = tonumber(ARGV[1])
	local window = tonumber(ARGV[2])
	local limit = tonumber(ARGV[3])
	local member = ARGV[4]

	redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
	local count = redis.call('ZCARD', key)
	if count >= limit then
		return 0
	end
	redis.call('ZADD', key, now, member)
	redis.call('PEXPIRE', key, math.ceil(window / 1000000))
	return 1
`)

func (p *RedisPubSub) CheckRateLimit(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	rateKey := "ratelimit:" + key
	now := time.Now().UnixNano()
	member := strconv.FormatInt(now, 10)

	windowNs := int64(window)
	result, err := rateLimitLuaScript.Run(ctx, p.client, []string{rateKey}, now, windowNs, limit, member).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (p *RedisPubSub) Close() error {
	return p.client.Close()
}

func (p *RedisPubSub) InvalidateToken(ctx context.Context, jti string, ttl time.Duration) error {
	key := "token:blacklist:" + jti
	return p.client.Set(ctx, key, "1", ttl).Err()
}

func (p *RedisPubSub) IsTokenInvalidated(ctx context.Context, jti string) (bool, error) {
	key := "token:blacklist:" + jti
	count, err := p.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
