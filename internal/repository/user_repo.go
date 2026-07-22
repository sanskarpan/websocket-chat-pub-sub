package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/websocket-chat/internal/model"
)

type UserRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, user *model.User) error {
	user.ID = uuid.New().String()
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()

	query := `
		INSERT INTO users (id, username, email, password_hash, display_name, avatar_url, status, created_at, updated_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := r.db.Exec(ctx, query,
		user.ID, user.Username, user.Email, user.PasswordHash, user.DisplayName,
		user.AvatarURL, user.Status, user.CreatedAt, user.UpdatedAt, user.Metadata,
	)
	return err
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (*model.User, error) {
	query := `
		SELECT id, username, email, password_hash, display_name, avatar_url, status, last_seen_at, created_at, updated_at, metadata
		FROM users WHERE id = $1
	`
	var user model.User
	err := r.db.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.DisplayName,
		&user.AvatarURL, &user.Status, &user.LastSeenAt, &user.CreatedAt, &user.UpdatedAt, &user.Metadata,
	)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	query := `
		SELECT id, username, email, password_hash, display_name, avatar_url, status, last_seen_at, created_at, updated_at, metadata
		FROM users WHERE username = $1
	`
	var user model.User
	err := r.db.QueryRow(ctx, query, username).Scan(
		&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.DisplayName,
		&user.AvatarURL, &user.Status, &user.LastSeenAt, &user.CreatedAt, &user.UpdatedAt, &user.Metadata,
	)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	query := `
		SELECT id, username, email, password_hash, display_name, avatar_url, status, last_seen_at, created_at, updated_at, metadata
		FROM users WHERE email = $1
	`
	var user model.User
	err := r.db.QueryRow(ctx, query, email).Scan(
		&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.DisplayName,
		&user.AvatarURL, &user.Status, &user.LastSeenAt, &user.CreatedAt, &user.UpdatedAt, &user.Metadata,
	)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) Update(ctx context.Context, user *model.User) error {
	user.UpdatedAt = time.Now()
	query := `
		UPDATE users SET username = $2, email = $3, display_name = $4, avatar_url = $5, 
		status = $6, last_seen_at = $7, updated_at = $8, metadata = $9
		WHERE id = $1
	`
	_, err := r.db.Exec(ctx, query,
		user.ID, user.Username, user.Email, user.DisplayName, user.AvatarURL,
		user.Status, user.LastSeenAt, user.UpdatedAt, user.Metadata,
	)
	return err
}

func (r *UserRepository) Search(ctx context.Context, query string, limit int) ([]*model.User, error) {
	sqlQuery := `
		SELECT id, username, email, password_hash, display_name, avatar_url, status, last_seen_at, created_at, updated_at, metadata
		FROM users 
		WHERE username ILIKE $1 OR display_name ILIKE $1
		LIMIT $2
	`
	rows, err := r.db.Query(ctx, sqlQuery, "%"+query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*model.User
	for rows.Next() {
		var user model.User
		err := rows.Scan(
			&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.DisplayName,
			&user.AvatarURL, &user.Status, &user.LastSeenAt, &user.CreatedAt, &user.UpdatedAt, &user.Metadata,
		)
		if err != nil {
			return nil, err
		}
		users = append(users, &user)
	}
	return users, nil
}
