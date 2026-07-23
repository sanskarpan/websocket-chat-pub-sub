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
	defer recordQueryDuration("create_room", time.Now())
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

func (r *RoomRepository) CreateRoomWithOwner(ctx context.Context, room *model.Room, owner *model.RoomMember) error {
	defer recordQueryDuration("create_room_with_owner", time.Now())
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	room.ID = uuid.New().String()
	room.CreatedAt = time.Now()
	room.UpdatedAt = time.Now()

	settings, err := json.Marshal(room.Settings)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO rooms (id, name, type, description, avatar_url, created_by, created_at, updated_at, archived_at, settings, member_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, room.ID, room.Name, room.Type, room.Description, room.AvatarURL, room.CreatedBy, room.CreatedAt, room.UpdatedAt, room.ArchivedAt, settings, room.MemberCount); err != nil {
		return err
	}

	owner.RoomID = room.ID
	owner.JoinedAt = time.Now()
	if owner.LastReadAt.IsZero() {
		owner.LastReadAt = time.Now()
	}
	notif, _ := json.Marshal(owner.Notifications)
	if _, err := tx.Exec(ctx, `
		INSERT INTO room_members (room_id, user_id, role, joined_at, last_read_at, notifications)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, owner.RoomID, owner.UserID, owner.Role, owner.JoinedAt, owner.LastReadAt, notif); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *RoomRepository) GetByID(ctx context.Context, id string) (*model.Room, error) {
	defer recordQueryDuration("get_room_by_id", time.Now())
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
	defer recordQueryDuration("update_room", time.Now())
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
	defer recordQueryDuration("delete_room", time.Now())
	query := `UPDATE rooms SET archived_at = $2 WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id, time.Now())
	return err
}

func (r *RoomRepository) GetUserRooms(ctx context.Context, userID string) ([]*model.Room, error) {
	defer recordQueryDuration("get_user_rooms", time.Now())
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return rooms, nil
}

func (r *RoomRepository) AddMember(ctx context.Context, member *model.RoomMember) error {
	defer recordQueryDuration("add_member", time.Now())
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

func (r *RoomRepository) JoinRoomTx(ctx context.Context, member *model.RoomMember) error {
	defer recordQueryDuration("join_room_tx", time.Now())
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	memberQuery := `
		INSERT INTO room_members (room_id, user_id, role, joined_at, last_read_at, notifications)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (room_id, user_id) DO UPDATE SET left_at = NULL, joined_at = COALESCE(room_members.joined_at, NOW())
	`
	notif, _ := json.Marshal(member.Notifications)
	if _, err := tx.Exec(ctx, memberQuery, member.RoomID, member.UserID, member.Role, member.JoinedAt, member.LastReadAt, notif); err != nil {
		return err
	}

	var result *time.Time
	row := tx.QueryRow(ctx, `SELECT joined_at FROM room_members WHERE room_id = $1 AND user_id = $2`, member.RoomID, member.UserID)
	if err := row.Scan(&result); err == nil {
		now := time.Now()
		if result != nil && result.After(member.JoinedAt.Add(-1*time.Second)) {
			updateQuery := `UPDATE rooms SET member_count = (SELECT COUNT(*) FROM room_members WHERE room_id = $1 AND left_at IS NULL), updated_at = $2 WHERE id = $1`
			if _, err := tx.Exec(ctx, updateQuery, member.RoomID, now); err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}

func (r *RoomRepository) LeaveRoomTx(ctx context.Context, roomID, userID string) error {
	defer recordQueryDuration("leave_room_tx", time.Now())
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `UPDATE room_members SET left_at = $3 WHERE room_id = $1 AND user_id = $2`, roomID, userID, time.Now()); err != nil {
		return err
	}

	updateQuery := `UPDATE rooms SET member_count = (SELECT COUNT(*) FROM room_members WHERE room_id = $1 AND left_at IS NULL), updated_at = NOW() WHERE id = $1`
	if _, err := tx.Exec(ctx, updateQuery, roomID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *RoomRepository) RemoveMember(ctx context.Context, roomID, userID string) error {
	defer recordQueryDuration("remove_member", time.Now())
	query := `UPDATE room_members SET left_at = $3 WHERE room_id = $1 AND user_id = $2`
	_, err := r.db.Exec(ctx, query, roomID, userID, time.Now())
	return err
}

func (r *RoomRepository) GetMembers(ctx context.Context, roomID string) ([]*model.RoomMember, error) {
	defer recordQueryDuration("get_members", time.Now())
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return members, nil
}

func (r *RoomRepository) IsMember(ctx context.Context, roomID, userID string) (bool, error) {
	defer recordQueryDuration("is_member", time.Now())
	query := `SELECT EXISTS(SELECT 1 FROM room_members WHERE room_id = $1 AND user_id = $2 AND left_at IS NULL)`
	var exists bool
	err := r.db.QueryRow(ctx, query, roomID, userID).Scan(&exists)
	return exists, err
}

func (r *RoomRepository) GetMember(ctx context.Context, roomID, userID string) (*model.RoomMember, error) {
	defer recordQueryDuration("get_member", time.Now())
	query := `
		SELECT room_id, user_id, role, joined_at, left_at, last_read_at, muted_until, banned_at, ban_reason, notifications
		FROM room_members WHERE room_id = $1 AND user_id = $2 AND left_at IS NULL
	`
	var member model.RoomMember
	var notif []byte
	err := r.db.QueryRow(ctx, query, roomID, userID).Scan(
		&member.RoomID, &member.UserID, &member.Role, &member.JoinedAt,
		&member.LeftAt, &member.LastReadAt, &member.MutedUntil, &member.BannedAt, &member.BanReason, &notif,
	)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(notif, &member.Notifications)
	return &member, nil
}

func (r *RoomRepository) IncrementMemberCount(ctx context.Context, roomID string) error {
	defer recordQueryDuration("increment_member_count", time.Now())
	query := `UPDATE rooms SET member_count = member_count + 1, updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, query, roomID)
	return err
}

func (r *RoomRepository) DecrementMemberCount(ctx context.Context, roomID string) error {
	defer recordQueryDuration("decrement_member_count", time.Now())
	query := `UPDATE rooms SET member_count = GREATEST(member_count - 1, 0), updated_at = NOW() WHERE id = $1`
	_, err := r.db.Exec(ctx, query, roomID)
	return err
}

func (r *RoomRepository) MarkRead(ctx context.Context, roomID, userID, messageID string) error {
	defer recordQueryDuration("mark_read", time.Now())
	query := `
		INSERT INTO read_receipts (room_id, user_id, message_id, read_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (room_id, user_id) DO UPDATE SET message_id = $3, read_at = NOW()
	`
	_, err := r.db.Exec(ctx, query, roomID, userID, messageID)
	return err
}
