package handlers_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/internal/repository"
)

var _ repository.IUserRepository = (*fakeUserRepo)(nil)
var _ repository.IRoomRepository = (*fakeRoomRepo)(nil)
var _ repository.IMessageRepository = (*fakeMessageRepo)(nil)

// ─── user repo ───────────────────────────────────────────────────────────────

type fakeUserRepo struct {
	mu    sync.RWMutex
	users map[string]*model.User
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{users: make(map[string]*model.User)}
}

func (r *fakeUserRepo) Create(_ context.Context, user *model.User) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if user.ID == "" {
		user.ID = "user-" + user.Username
	}
	user.CreatedAt = time.Now(); user.UpdatedAt = time.Now()
	r.users[user.ID] = user
	r.users["username:"+user.Username] = user
	r.users["email:"+user.Email] = user
	return nil
}
func (r *fakeUserRepo) GetByID(_ context.Context, id string) (*model.User, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if u, ok := r.users[id]; ok { return u, nil }
	return nil, errors.New("user not found")
}
func (r *fakeUserRepo) GetByUsername(_ context.Context, username string) (*model.User, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if u, ok := r.users["username:"+username]; ok { return u, nil }
	return nil, errors.New("user not found")
}
func (r *fakeUserRepo) GetByEmail(_ context.Context, email string) (*model.User, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if u, ok := r.users["email:"+email]; ok { return u, nil }
	return nil, errors.New("user not found")
}
func (r *fakeUserRepo) Update(_ context.Context, user *model.User) error {
	r.mu.Lock(); defer r.mu.Unlock()
	user.UpdatedAt = time.Now(); r.users[user.ID] = user; return nil
}
func (r *fakeUserRepo) Search(_ context.Context, query string, limit int) ([]*model.User, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	var out []*model.User
	for k, u := range r.users {
		if strings.HasPrefix(k, "username:") || strings.HasPrefix(k, "email:") { continue }
		if len(out) >= limit { break }
		out = append(out, u)
	}
	return out, nil
}

// ─── room repo ────────────────────────────────────────────────────────────────

type fakeRoomRepo struct {
	mu      sync.RWMutex
	rooms   map[string]*model.Room
	members map[string]*model.RoomMember
}

func newFakeRoomRepo() *fakeRoomRepo {
	return &fakeRoomRepo{rooms: make(map[string]*model.Room), members: make(map[string]*model.RoomMember)}
}

func roomKey(roomID, userID string) string { return roomID + ":" + userID }

func (r *fakeRoomRepo) Create(_ context.Context, room *model.Room) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if room.ID == "" { room.ID = "room-" + room.Name }
	room.CreatedAt = time.Now(); room.UpdatedAt = time.Now()
	r.rooms[room.ID] = room; return nil
}
func (r *fakeRoomRepo) CreateRoomWithOwner(_ context.Context, room *model.Room, owner *model.RoomMember) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if room.ID == "" { room.ID = "room-" + room.Name }
	room.CreatedAt = time.Now(); room.UpdatedAt = time.Now()
	r.rooms[room.ID] = room
	owner.RoomID = room.ID; owner.JoinedAt = time.Now()
	r.members[roomKey(owner.RoomID, owner.UserID)] = owner
	return nil
}
func (r *fakeRoomRepo) GetByID(_ context.Context, id string) (*model.Room, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if room, ok := r.rooms[id]; ok { return room, nil }
	return nil, errors.New("room not found")
}
func (r *fakeRoomRepo) Update(_ context.Context, room *model.Room) error {
	r.mu.Lock(); defer r.mu.Unlock(); r.rooms[room.ID] = room; return nil
}
func (r *fakeRoomRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	now := time.Now()
	if room, ok := r.rooms[id]; ok { room.ArchivedAt = &now }
	return nil
}
func (r *fakeRoomRepo) GetUserRooms(_ context.Context, userID string) ([]*model.Room, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	var rooms []*model.Room
	for _, m := range r.members {
		if m.UserID == userID && m.LeftAt == nil {
			if room, ok := r.rooms[m.RoomID]; ok { rooms = append(rooms, room) }
		}
	}
	return rooms, nil
}
func (r *fakeRoomRepo) AddMember(_ context.Context, member *model.RoomMember) error {
	r.mu.Lock(); defer r.mu.Unlock()
	member.JoinedAt = time.Now(); r.members[roomKey(member.RoomID, member.UserID)] = member; return nil
}
func (r *fakeRoomRepo) JoinRoomTx(_ context.Context, member *model.RoomMember) error {
	r.mu.Lock(); defer r.mu.Unlock()
	member.JoinedAt = time.Now(); r.members[roomKey(member.RoomID, member.UserID)] = member
	if room, ok := r.rooms[member.RoomID]; ok { room.MemberCount++; room.UpdatedAt = time.Now() }
	return nil
}
func (r *fakeRoomRepo) LeaveRoomTx(_ context.Context, roomID, userID string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if m, ok := r.members[roomKey(roomID, userID)]; ok { now := time.Now(); m.LeftAt = &now }
	if room, ok := r.rooms[roomID]; ok && room.MemberCount > 0 { room.MemberCount--; room.UpdatedAt = time.Now() }
	return nil
}
func (r *fakeRoomRepo) RemoveMember(_ context.Context, roomID, userID string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if m, ok := r.members[roomKey(roomID, userID)]; ok { now := time.Now(); m.LeftAt = &now }
	return nil
}
func (r *fakeRoomRepo) GetMembers(_ context.Context, roomID string) ([]*model.RoomMember, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	var out []*model.RoomMember
	for _, m := range r.members {
		if m.RoomID == roomID && m.LeftAt == nil { out = append(out, m) }
	}
	return out, nil
}
func (r *fakeRoomRepo) GetMember(_ context.Context, roomID, userID string) (*model.RoomMember, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if m, ok := r.members[roomKey(roomID, userID)]; ok && m.LeftAt == nil { return m, nil }
	return nil, errors.New("not a member")
}
func (r *fakeRoomRepo) GetMemberRecord(_ context.Context, roomID, userID string) (*model.RoomMember, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if m, ok := r.members[roomKey(roomID, userID)]; ok { return m, nil }
	return nil, errors.New("no member record")
}
func (r *fakeRoomRepo) IsMember(_ context.Context, roomID, userID string) (bool, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if m, ok := r.members[roomKey(roomID, userID)]; ok && m.LeftAt == nil { return true, nil }
	return false, nil
}
func (r *fakeRoomRepo) IncrementMemberCount(_ context.Context, roomID string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if room, ok := r.rooms[roomID]; ok { room.MemberCount++; room.UpdatedAt = time.Now() }
	return nil
}
func (r *fakeRoomRepo) DecrementMemberCount(_ context.Context, roomID string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if room, ok := r.rooms[roomID]; ok && room.MemberCount > 0 { room.MemberCount--; room.UpdatedAt = time.Now() }
	return nil
}
func (r *fakeRoomRepo) MarkRead(_ context.Context, roomID, userID, messageID string) error { return nil }

// BanMember is used in tests to set BannedAt on a member entry.
func (r *fakeRoomRepo) BanMember(roomID, userID string) {
	r.mu.Lock(); defer r.mu.Unlock()
	if m, ok := r.members[roomKey(roomID, userID)]; ok {
		now := time.Now(); m.BannedAt = &now
	}
}

// ─── message repo ─────────────────────────────────────────────────────────────

var hMsgSeq uint64

type fakeMessageRepo struct {
	mu       sync.RWMutex
	messages map[string]*model.Message
}

func newFakeMessageRepo() *fakeMessageRepo {
	return &fakeMessageRepo{messages: make(map[string]*model.Message)}
}

func (r *fakeMessageRepo) Create(_ context.Context, msg *model.Message) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if msg.ID == "" {
		seq := atomic.AddUint64(&hMsgSeq, 1)
		msg.ID = fmt.Sprintf("msg-%s-%s-%d", msg.RoomID, msg.UserID, seq)
	}
	msg.CreatedAt = time.Now(); r.messages[msg.ID] = msg; return nil
}
func (r *fakeMessageRepo) GetByID(_ context.Context, id string) (*model.Message, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if msg, ok := r.messages[id]; ok { return msg, nil }
	return nil, errors.New("message not found")
}
func (r *fakeMessageRepo) GetByRoom(_ context.Context, roomID string, limit int, before *time.Time) ([]*model.Message, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	var out []*model.Message
	for _, m := range r.messages {
		if m.RoomID == roomID && m.DeletedAt == nil { out = append(out, m) }
	}
	return out, nil
}
func (r *fakeMessageRepo) Update(_ context.Context, msg *model.Message) error {
	r.mu.Lock(); defer r.mu.Unlock(); now := time.Now(); msg.EditedAt = &now; r.messages[msg.ID] = msg; return nil
}
func (r *fakeMessageRepo) UpdateReactions(_ context.Context, msgID string, reactions map[string][]string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if msg, ok := r.messages[msgID]; ok { msg.Reactions = reactions }
	return nil
}
func (r *fakeMessageRepo) UpdateReactionsTx(_ context.Context, msgID string, transform func(map[string][]string) (map[string][]string, error)) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if msg, ok := r.messages[msgID]; ok {
		if msg.Reactions == nil { msg.Reactions = make(map[string][]string) }
		newR, err := transform(msg.Reactions)
		if err != nil { return err }
		msg.Reactions = newR
	}
	return nil
}
func (r *fakeMessageRepo) Delete(_ context.Context, id, deletedBy string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if msg, ok := r.messages[id]; ok { now := time.Now(); msg.DeletedAt = &now; msg.DeletedBy = &deletedBy }
	return nil
}
func (r *fakeMessageRepo) GetThread(_ context.Context, parentID string, limit int) ([]*model.Message, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	var out []*model.Message
	for _, m := range r.messages {
		if m.ParentID != nil && *m.ParentID == parentID && m.DeletedAt == nil { out = append(out, m) }
	}
	return out, nil
}

// ─── token invalidator ────────────────────────────────────────────────────────

type fakeInvalidator struct {
	mu   sync.Mutex
	data map[string]bool
}

func newFakeInvalidator() *fakeInvalidator {
	return &fakeInvalidator{data: make(map[string]bool)}
}

func (f *fakeInvalidator) InvalidateToken(_ context.Context, jti string, _ time.Duration) error {
	f.mu.Lock(); defer f.mu.Unlock(); f.data[jti] = true; return nil
}
func (f *fakeInvalidator) IsTokenInvalidated(_ context.Context, jti string) (bool, error) {
	f.mu.Lock(); defer f.mu.Unlock(); return f.data[jti], nil
}
