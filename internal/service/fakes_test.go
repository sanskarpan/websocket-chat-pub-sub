package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/internal/repository"
)

var _ repository.IUserRepository = (*FakeUserRepository)(nil)
var _ repository.IRoomRepository = (*FakeRoomRepository)(nil)
var _ repository.IMessageRepository = (*FakeMessageRepository)(nil)

type FakeUserRepository struct {
	mu      sync.RWMutex
	users   map[string]*model.User
	failGet bool
}

func NewFakeUserRepository() *FakeUserRepository {
	return &FakeUserRepository{
		users: make(map[string]*model.User),
	}
}

func (r *FakeUserRepository) Create(ctx context.Context, user *model.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.users[user.ID]; exists {
		return errors.New("user already exists")
	}
	if user.ID == "" {
		user.ID = "user-" + user.Username
	}
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()
	r.users[user.ID] = user
	r.users["username:"+user.Username] = user
	r.users["email:"+user.Email] = user
	return nil
}

func (r *FakeUserRepository) GetByID(ctx context.Context, id string) (*model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if u, ok := r.users[id]; ok {
		return u, nil
	}
	return nil, errors.New("user not found")
}

func (r *FakeUserRepository) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if u, ok := r.users["username:"+username]; ok {
		return u, nil
	}
	return nil, errors.New("user not found")
}

func (r *FakeUserRepository) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if u, ok := r.users["email:"+email]; ok {
		return u, nil
	}
	return nil, errors.New("user not found")
}

func (r *FakeUserRepository) Update(ctx context.Context, user *model.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.users[user.ID]; !exists {
		return errors.New("user not found")
	}
	user.UpdatedAt = time.Now()
	r.users[user.ID] = user
	return nil
}

func (r *FakeUserRepository) Search(ctx context.Context, query string, limit int) ([]*model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var results []*model.User
	for k, u := range r.users {
		if strings.HasPrefix(k, "username:") || strings.HasPrefix(k, "email:") {
			continue
		}
		if len(results) >= limit {
			break
		}
		results = append(results, u)
	}
	return results, nil
}

type FakeRoomRepository struct {
	mu        sync.RWMutex
	rooms     map[string]*model.Room
	members  map[string]*model.RoomMember
	failGet  bool
}

func NewFakeRoomRepository() *FakeRoomRepository {
	return &FakeRoomRepository{
		rooms:    make(map[string]*model.Room),
		members:  make(map[string]*model.RoomMember),
	}
}

func key(roomID, userID string) string {
	return roomID + ":" + userID
}

func (r *FakeRoomRepository) Create(ctx context.Context, room *model.Room) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if room.ID == "" {
		room.ID = "room-" + room.Name
	}
	room.CreatedAt = time.Now()
	room.UpdatedAt = time.Now()
	r.rooms[room.ID] = room
	return nil
}

func (r *FakeRoomRepository) CreateRoomWithOwner(ctx context.Context, room *model.Room, owner *model.RoomMember) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if room.ID == "" {
		room.ID = "room-" + room.Name
	}
	room.CreatedAt = time.Now()
	room.UpdatedAt = time.Now()
	r.rooms[room.ID] = room
	owner.RoomID = room.ID
	owner.JoinedAt = time.Now()
	r.members[key(owner.RoomID, owner.UserID)] = owner
	return nil
}

func (r *FakeRoomRepository) GetByID(ctx context.Context, id string) (*model.Room, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if room, ok := r.rooms[id]; ok {
		if room.ArchivedAt != nil {
			return nil, errors.New("room archived")
		}
		return room, nil
	}
	return nil, errors.New("room not found")
}

func (r *FakeRoomRepository) Update(ctx context.Context, room *model.Room) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rooms[room.ID] = room
	return nil
}

func (r *FakeRoomRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if room, ok := r.rooms[id]; ok {
		room.ArchivedAt = &now
	}
	return nil
}

func (r *FakeRoomRepository) GetUserRooms(ctx context.Context, userID string) ([]*model.Room, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var rooms []*model.Room
	for _, m := range r.members {
		if m.UserID == userID && m.LeftAt == nil {
			if room, ok := r.rooms[m.RoomID]; ok {
				rooms = append(rooms, room)
			}
		}
	}
	return rooms, nil
}

func (r *FakeRoomRepository) AddMember(ctx context.Context, member *model.RoomMember) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	member.JoinedAt = time.Now()
	r.members[key(member.RoomID, member.UserID)] = member
	return nil
}

func (r *FakeRoomRepository) JoinRoomTx(ctx context.Context, member *model.RoomMember) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	member.JoinedAt = time.Now()
	r.members[key(member.RoomID, member.UserID)] = member
	if room, ok := r.rooms[member.RoomID]; ok {
		room.MemberCount++
		room.UpdatedAt = time.Now()
	}
	return nil
}

func (r *FakeRoomRepository) LeaveRoomTx(ctx context.Context, roomID, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.members[key(roomID, userID)]; ok {
		now := time.Now()
		m.LeftAt = &now
	}
	if room, ok := r.rooms[roomID]; ok && room.MemberCount > 0 {
		room.MemberCount--
		room.UpdatedAt = time.Now()
	}
	return nil
}

func (r *FakeRoomRepository) RemoveMember(ctx context.Context, roomID, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.members[key(roomID, userID)]; ok {
		now := time.Now()
		m.LeftAt = &now
	}
	return nil
}

func (r *FakeRoomRepository) GetMembers(ctx context.Context, roomID string) ([]*model.RoomMember, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var members []*model.RoomMember
	for _, m := range r.members {
		if m.RoomID == roomID && m.LeftAt == nil {
			members = append(members, m)
		}
	}
	return members, nil
}

func (r *FakeRoomRepository) GetMember(ctx context.Context, roomID, userID string) (*model.RoomMember, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if m, ok := r.members[key(roomID, userID)]; ok && m.LeftAt == nil {
		return m, nil
	}
	return nil, errors.New("not a member")
}

func (r *FakeRoomRepository) IsMember(ctx context.Context, roomID, userID string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if m, ok := r.members[key(roomID, userID)]; ok && m.LeftAt == nil {
		return true, nil
	}
	return false, nil
}

func (r *FakeRoomRepository) IncrementMemberCount(ctx context.Context, roomID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if room, ok := r.rooms[roomID]; ok {
		room.MemberCount++
		room.UpdatedAt = time.Now()
	}
	return nil
}

func (r *FakeRoomRepository) DecrementMemberCount(ctx context.Context, roomID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if room, ok := r.rooms[roomID]; ok && room.MemberCount > 0 {
		room.MemberCount--
		room.UpdatedAt = time.Now()
	}
	return nil
}

func (r *FakeRoomRepository) MarkRead(ctx context.Context, roomID, userID, messageID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return nil
}

type FakeMessageRepository struct {
	mu       sync.RWMutex
	messages map[string]*model.Message
}

func NewFakeMessageRepository() *FakeMessageRepository {
	return &FakeMessageRepository{
		messages: make(map[string]*model.Message),
	}
}

func (r *FakeMessageRepository) Create(ctx context.Context, msg *model.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if msg.ID == "" {
		msg.ID = "msg-" + msg.RoomID + "-" + msg.UserID
	}
	msg.CreatedAt = time.Now()
	r.messages[msg.ID] = msg
	return nil
}

func (r *FakeMessageRepository) GetByID(ctx context.Context, id string) (*model.Message, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if msg, ok := r.messages[id]; ok {
		return msg, nil
	}
	return nil, errors.New("message not found")
}

func (r *FakeMessageRepository) GetByRoom(ctx context.Context, roomID string, limit int, before *time.Time) ([]*model.Message, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var msgs []*model.Message
	for _, m := range r.messages {
		if m.RoomID == roomID && m.DeletedAt == nil {
			msgs = append(msgs, m)
		}
	}
	return msgs, nil
}

func (r *FakeMessageRepository) Update(ctx context.Context, msg *model.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	msg.EditedAt = &now
	r.messages[msg.ID] = msg
	return nil
}

func (r *FakeMessageRepository) UpdateReactions(ctx context.Context, msgID string, reactions map[string][]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if msg, ok := r.messages[msgID]; ok {
		now := time.Now()
		msg.EditedAt = &now
		msg.Reactions = reactions
	}
	return nil
}

func (r *FakeMessageRepository) UpdateReactionsTx(ctx context.Context, msgID string, transform func(current map[string][]string) (map[string][]string, error)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if msg, ok := r.messages[msgID]; ok {
		if msg.Reactions == nil {
			msg.Reactions = make(map[string][]string)
		}
		newReactions, err := transform(msg.Reactions)
		if err != nil {
			return err
		}
		msg.Reactions = newReactions
	}
	return nil
}

func (r *FakeMessageRepository) Delete(ctx context.Context, id, deletedBy string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if msg, ok := r.messages[id]; ok {
		now := time.Now()
		msg.DeletedAt = &now
		msg.DeletedBy = &deletedBy
	}
	return nil
}

func (r *FakeMessageRepository) GetThread(ctx context.Context, parentID string, limit int) ([]*model.Message, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var msgs []*model.Message
	for _, m := range r.messages {
		if m.ParentID != nil && *m.ParentID == parentID && m.DeletedAt == nil {
			msgs = append(msgs, m)
		}
	}
	return msgs, nil
}