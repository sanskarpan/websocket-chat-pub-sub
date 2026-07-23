package repository

import (
	"context"
	"time"

	"github.com/websocket-chat/internal/model"
)

type IUserRepository interface {
	Create(ctx context.Context, user *model.User) error
	GetByID(ctx context.Context, id string) (*model.User, error)
	GetByUsername(ctx context.Context, username string) (*model.User, error)
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	Update(ctx context.Context, user *model.User) error
	Search(ctx context.Context, query string, limit int) ([]*model.User, error)
}

type IRoomRepository interface {
	Create(ctx context.Context, room *model.Room) error
	GetByID(ctx context.Context, id string) (*model.Room, error)
	Update(ctx context.Context, room *model.Room) error
	Delete(ctx context.Context, id string) error
	GetUserRooms(ctx context.Context, userID string) ([]*model.Room, error)
	AddMember(ctx context.Context, member *model.RoomMember) error
	RemoveMember(ctx context.Context, roomID, userID string) error
	GetMembers(ctx context.Context, roomID string) ([]*model.RoomMember, error)
	GetMember(ctx context.Context, roomID, userID string) (*model.RoomMember, error)
	IsMember(ctx context.Context, roomID, userID string) (bool, error)
	IncrementMemberCount(ctx context.Context, roomID string) error
	DecrementMemberCount(ctx context.Context, roomID string) error
	MarkRead(ctx context.Context, roomID, userID, messageID string) error
	JoinRoomTx(ctx context.Context, member *model.RoomMember) error
	LeaveRoomTx(ctx context.Context, roomID, userID string) error
}

type IMessageRepository interface {
	Create(ctx context.Context, msg *model.Message) error
	GetByID(ctx context.Context, id string) (*model.Message, error)
	GetByRoom(ctx context.Context, roomID string, limit int, before *time.Time) ([]*model.Message, error)
	Update(ctx context.Context, msg *model.Message) error
	UpdateReactions(ctx context.Context, msgID string, reactions map[string][]string) error
	UpdateReactionsTx(ctx context.Context, msgID string, reactions map[string][]string) error
	Delete(ctx context.Context, id, deletedBy string) error
	GetThread(ctx context.Context, parentID string, limit int) ([]*model.Message, error)
}