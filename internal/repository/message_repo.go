package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/pkg/snowflake"
)

type MessageRepository struct {
	db *pgxpool.Pool
}

func NewMessageRepository(db *pgxpool.Pool) *MessageRepository {
	return &MessageRepository{db: db}
}

func (r *MessageRepository) Create(ctx context.Context, msg *model.Message) error {
	defer recordQueryDuration("create_message", time.Now())
	msg.ID = snowflake.Generate().String()
	msg.CreatedAt = time.Now()

	reactions, _ := json.Marshal(msg.Reactions)
	attachments, _ := json.Marshal(msg.Attachments)
	metadata, _ := json.Marshal(msg.Metadata)

	query := `
		INSERT INTO messages (id, room_id, user_id, content, content_type, parent_id, thread_count, 
			edited_at, deleted_at, deleted_by, reactions, attachments, metadata, created_at, client_timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	_, err := r.db.Exec(ctx, query,
		msg.ID, msg.RoomID, msg.UserID, msg.Content, msg.ContentType, msg.ParentID,
		msg.ThreadCount, msg.EditedAt, msg.DeletedAt, msg.DeletedBy, reactions, attachments, metadata,
		msg.CreatedAt, msg.ClientTimestamp,
	)
	return err
}

func (r *MessageRepository) GetByID(ctx context.Context, id string) (*model.Message, error) {
	defer recordQueryDuration("get_message_by_id", time.Now())
	query := `
		SELECT id, room_id, user_id, content, content_type, parent_id, thread_count,
			edited_at, deleted_at, deleted_by, reactions, attachments, metadata, created_at, client_timestamp
		FROM messages WHERE id = $1
	`
	var msg model.Message
	var reactions, attachments, metadata []byte
	err := r.db.QueryRow(ctx, query, id).Scan(
		&msg.ID, &msg.RoomID, &msg.UserID, &msg.Content, &msg.ContentType, &msg.ParentID,
		&msg.ThreadCount, &msg.EditedAt, &msg.DeletedAt, &msg.DeletedBy, &reactions, &attachments, &metadata,
		&msg.CreatedAt, &msg.ClientTimestamp,
	)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(reactions, &msg.Reactions)
	json.Unmarshal(attachments, &msg.Attachments)
	json.Unmarshal(metadata, &msg.Metadata)
	return &msg, nil
}

func (r *MessageRepository) GetByRoom(ctx context.Context, roomID string, limit int, before *time.Time) ([]*model.Message, error) {
	defer recordQueryDuration("get_messages_by_room", time.Now())
	var query string
	var args []interface{}

	if before != nil {
		query = `
			SELECT id, room_id, user_id, content, content_type, parent_id, thread_count,
				edited_at, deleted_at, deleted_by, reactions, attachments, metadata, created_at, client_timestamp
			FROM messages WHERE room_id = $1 AND created_at < $2 AND deleted_at IS NULL
			ORDER BY created_at DESC LIMIT $3
		`
		args = []interface{}{roomID, before, limit}
	} else {
		query = `
			SELECT id, room_id, user_id, content, content_type, parent_id, thread_count,
				edited_at, deleted_at, deleted_by, reactions, attachments, metadata, created_at, client_timestamp
			FROM messages WHERE room_id = $1 AND deleted_at IS NULL
			ORDER BY created_at DESC LIMIT $2
		`
		args = []interface{}{roomID, limit}
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*model.Message
	for rows.Next() {
		var msg model.Message
		var reactions, attachments, metadata []byte
		err := rows.Scan(
			&msg.ID, &msg.RoomID, &msg.UserID, &msg.Content, &msg.ContentType, &msg.ParentID,
			&msg.ThreadCount, &msg.EditedAt, &msg.DeletedAt, &msg.DeletedBy, &reactions, &attachments, &metadata,
			&msg.CreatedAt, &msg.ClientTimestamp,
		)
		if err != nil {
			return nil, err
		}
		json.Unmarshal(reactions, &msg.Reactions)
		json.Unmarshal(attachments, &msg.Attachments)
		json.Unmarshal(metadata, &msg.Metadata)
		messages = append(messages, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func (r *MessageRepository) Update(ctx context.Context, msg *model.Message) error {
	defer recordQueryDuration("update_message", time.Now())
	query := `
		UPDATE messages SET content = $2, edited_at = $3, reactions = $4, attachments = $5, metadata = $6
		WHERE id = $1
	`
	now := time.Now()
	msg.EditedAt = &now
	reactions, _ := json.Marshal(msg.Reactions)
	attachments, _ := json.Marshal(msg.Attachments)
	metadata, _ := json.Marshal(msg.Metadata)

	_, err := r.db.Exec(ctx, query, msg.ID, msg.Content, msg.EditedAt, reactions, attachments, metadata)
	return err
}

func (r *MessageRepository) UpdateReactions(ctx context.Context, msgID string, reactions map[string][]string) error {
	defer recordQueryDuration("update_reactions", time.Now())
	reactionsJSON, err := json.Marshal(reactions)
	if err != nil {
		return err
	}
	query := `UPDATE messages SET reactions = $2 WHERE id = $1`
	_, err = r.db.Exec(ctx, query, msgID, reactionsJSON)
	return err
}

func (r *MessageRepository) UpdateReactionsTx(ctx context.Context, msgID string, transform func(current map[string][]string) (map[string][]string, error)) error {
	defer recordQueryDuration("update_reactions_tx", time.Now())
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var existing []byte
	row := tx.QueryRow(ctx, `SELECT reactions FROM messages WHERE id = $1 FOR UPDATE`, msgID)
	if err := row.Scan(&existing); err != nil {
		return err
	}

	var current map[string][]string
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &current); err != nil {
			return err
		}
	}
	if current == nil {
		current = make(map[string][]string)
	}

	newReactions, err := transform(current)
	if err != nil {
		return err
	}

	reactionsJSON, err := json.Marshal(newReactions)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `UPDATE messages SET reactions = $2 WHERE id = $1`, msgID, reactionsJSON); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *MessageRepository) Delete(ctx context.Context, id, deletedBy string) error {
	defer recordQueryDuration("delete_message", time.Now())
	query := `UPDATE messages SET deleted_at = $2, deleted_by = $3 WHERE id = $1 AND deleted_at IS NULL`
	_, err := r.db.Exec(ctx, query, id, time.Now(), deletedBy)
	return err
}

func (r *MessageRepository) GetThread(ctx context.Context, parentID string, limit int) ([]*model.Message, error) {
	defer recordQueryDuration("get_thread", time.Now())
	query := `
		SELECT id, room_id, user_id, content, content_type, parent_id, thread_count,
			edited_at, deleted_at, deleted_by, reactions, attachments, metadata, created_at, client_timestamp
		FROM messages WHERE parent_id = $1 AND deleted_at IS NULL
		ORDER BY created_at ASC LIMIT $2
	`
	rows, err := r.db.Query(ctx, query, parentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*model.Message
	for rows.Next() {
		var msg model.Message
		var reactions, attachments, metadata []byte
		err := rows.Scan(
			&msg.ID, &msg.RoomID, &msg.UserID, &msg.Content, &msg.ContentType, &msg.ParentID,
			&msg.ThreadCount, &msg.EditedAt, &msg.DeletedAt, &msg.DeletedBy, &reactions, &attachments, &metadata,
			&msg.CreatedAt, &msg.ClientTimestamp,
		)
		if err != nil {
			return nil, err
		}
		json.Unmarshal(reactions, &msg.Reactions)
		json.Unmarshal(attachments, &msg.Attachments)
		json.Unmarshal(metadata, &msg.Metadata)
		messages = append(messages, &msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}
