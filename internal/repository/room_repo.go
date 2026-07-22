package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/websocket-chat/internal/model"
)

type RoomRepository struct {
	db *pgxpool.Pool
}

func NewRoomRepository(db *pgxpool.Pool) *RoomRepository {
	return &RoomRepository{db: db}
}

func (r *RoomRepository) Create(ctx context.Context, room *model.Room) error {
	room.ID = uuid.New().String()
	room.CreatedAt = time.Now()
	room.UpdatedAt = time.Now()

	settings, err := json.Marshal(room.Settings)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO rooms (id, name, type, description, avatar_url, created_by, created_at, updated_at, archived_at, settings, member_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err = r.db.Exec(ctx, query,
		room.ID, room.Name, room.Type, room.Description, room.AvatarURL,
		room.CreatedBy, room.CreatedAt, room.UpdatedAt, room.ArchivedAt, settings, room.MemberCount,
	)
	return err
}

func (r *RoomRepository) GetByID(ctx context.Context, id string) (*model.Room, error) {
	query := `
		SELECT id, name, type, description, avatar_url, created_by, created_at, updated_at, archived_at, settings, member_count
		FROM rooms WHERE id = $1 AND archived_at IS NULL
	`
	var room model.Room
	var settings []byte
	err := r.db.QueryRow(ctx, query, id).Scan(
		&room.ID, &room.Name, &room.Type, &room.Description, &room.AvatarURL,
		&room.CreatedBy, &room.CreatedAt, &room.UpdatedAt, &room.ArchivedAt, &settings, &room.MemberCount,
	)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(settings, &room.Settings)
	return &room, nil
}

func (r *RoomRepository) Update(ctx context.Context, room *model.Room) error {
	room.UpdatedAt = time.Now()
	settings, err := json.Marshal(room.Settings)
	if err != nil {
		return err
	}

	query := `
		UPDATE rooms SET name = $2, description = $3, avatar_url = $4, updated_at = $5, settings = $6, member_count = $7
		WHERE id = $1
	`
	_, err = r.db.Exec(ctx, query,
		room.ID, room.Name, room.Description, room.AvatarURL, room.UpdatedAt, settings, room.MemberCount,
	)
	return err
}

func (r *RoomRepository) Delete(ctx context.Context, id string) error {
	query := `UPDATE rooms SET archived_at = $2 WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id, time.Now())
	return err
}

func (r *RoomRepository) GetUserRooms(ctx context.Context, userID string) ([]*model.Room, error) {
	query := `
		SELECT r.id, r.name, r.type, r.description, r.avatar_url, r.created_by, r.created_at, r.updated_at, r.archived_at, r.settings, r.member_count
		FROM rooms r
		JOIN room_members rm ON r.id = rm.room_id
		WHERE rm.user_id = $1 AND rm.left_at IS NULL AND r.archived_at IS NULL
		ORDER BY r.updated_at DESC
	`
	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rooms []*model.Room
	for rows.Next() {
		var room model.Room
		var settings []byte
		err := rows.Scan(
			&room.ID, &room.Name, &room.Type, &room.Description, &room.AvatarURL,
			&room.CreatedBy, &room.CreatedAt, &room.UpdatedAt, &room.ArchivedAt, &settings, &room.MemberCount,
		)
		if err != nil {
			return nil, err
		}
		json.Unmarshal(settings, &room.Settings)
		rooms = append(rooms, &room)
	}
	return rooms, nil
}

func (r *RoomRepository) AddMember(ctx context.Context, member *model.RoomMember) error {
	query := `
		INSERT INTO room_members (room_id, user_id, role, joined_at, last_read_at, notifications)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (room_id, user_id) DO UPDATE SET left_at = NULL, joined_at = COALESCE(room_members.joined_at, NOW())
	`
	notif, _ := json.Marshal(member.Notifications)
	_, err := r.db.Exec(ctx, query,
		member.RoomID, member.UserID, member.Role, member.JoinedAt, member.LastReadAt, notif,
	)
	return err
}

func (r *RoomRepository) RemoveMember(ctx context.Context, roomID, userID string) error {
	query := `UPDATE room_members SET left_at = $3 WHERE room_id = $1 AND user_id = $2`
	_, err := r.db.Exec(ctx, query, roomID, userID, time.Now())
	return err
}

func (r *RoomRepository) GetMembers(ctx context.Context, roomID string) ([]*model.RoomMember, error) {
	query := `
		SELECT room_id, user_id, role, joined_at, left_at, last_read_at, muted_until, banned_at, ban_reason, notifications
		FROM room_members WHERE room_id = $1 AND left_at IS NULL
	`
	rows, err := r.db.Query(ctx, query, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []*model.RoomMember
	for rows.Next() {
		var member model.RoomMember
		var notif []byte
		err := rows.Scan(
			&member.RoomID, &member.UserID, &member.Role, &member.JoinedAt,
			&member.LeftAt, &member.LastReadAt, &member.MutedUntil, &member.BannedAt, &member.BanReason, &notif,
		)
		if err != nil {
			return nil, err
		}
		json.Unmarshal(notif, &member.Notifications)
		members = append(members, &member)
	}
	return members, nil
}

func (r *RoomRepository) IsMember(ctx context.Context, roomID, userID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id = $1 AND user_id = $2 AND left_at IS NULL)`
	var exists bool
	err := r.db.QueryRow(ctx, query, roomID, userID).Scan(&exists)
	return exists, err
}
