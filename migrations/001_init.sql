-- migrations/001_init.sql

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(30) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    display_name VARCHAR(100),
    avatar_url TEXT,
    status VARCHAR(20) DEFAULT 'offline',
    last_seen_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    metadata JSONB DEFAULT '{}',
    
    CONSTRAINT username_format CHECK (username ~ '^[a-zA-Z0-9_-]{3,30}$'),
    CONSTRAINT email_format CHECK (email ~* '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$')
);

CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_status ON users(status) WHERE status != 'offline';

CREATE TABLE IF NOT EXISTS rooms (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    type VARCHAR(20) NOT NULL CHECK (type IN ('direct', 'group', 'channel')),
    description TEXT,
    avatar_url TEXT,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    archived_at TIMESTAMPTZ,
    settings JSONB DEFAULT '{
        "allow_reactions": true,
        "allow_threads": true,
        "message_retention_days": 0,
        "slow_mode_seconds": 0,
        "require_approval": false,
        "only_admins_can_post": false
    }',
    member_count INT DEFAULT 0
);

CREATE INDEX idx_rooms_type ON rooms(type);
CREATE INDEX idx_rooms_created_by ON rooms(created_by);
CREATE INDEX idx_rooms_archived ON rooms(archived_at) WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS room_members (
    room_id UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(20) NOT NULL DEFAULT 'member' 
        CHECK (role IN ('owner', 'admin', 'moderator', 'member')),
    joined_at TIMESTAMPTZ DEFAULT NOW(),
    left_at TIMESTAMPTZ,
    last_read_at TIMESTAMPTZ DEFAULT NOW(),
    muted_until TIMESTAMPTZ,
    banned_at TIMESTAMPTZ,
    ban_reason TEXT,
    notifications JSONB DEFAULT '{"enabled": true, "mentions_only": false, "sound": true}',
    
    PRIMARY KEY (room_id, user_id)
);

CREATE INDEX idx_room_members_user ON room_members(user_id) WHERE left_at IS NULL;
CREATE INDEX idx_room_members_room ON room_members(room_id) WHERE left_at IS NULL;
CREATE INDEX idx_room_members_banned ON room_members(room_id, banned_at) WHERE banned_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS messages (
    id BIGINT NOT NULL,
    room_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id),
    content TEXT NOT NULL,
    content_type VARCHAR(20) DEFAULT 'text',
    parent_id BIGINT,
    thread_count INT DEFAULT 0,
    edited_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    deleted_by UUID REFERENCES users(id),
    reactions JSONB DEFAULT '{}',
    attachments JSONB DEFAULT '[]',
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    client_timestamp TIMESTAMPTZ,
    
    PRIMARY KEY (id, created_at)
);

CREATE INDEX idx_messages_room_created ON messages(room_id, created_at DESC);
CREATE INDEX idx_messages_user ON messages(user_id, created_at DESC);
CREATE INDEX idx_messages_parent ON messages(parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX idx_messages_search ON messages USING GIN(to_tsvector('english', content));

CREATE TABLE IF NOT EXISTS read_receipts (
    room_id UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id BIGINT NOT NULL,
    read_at TIMESTAMPTZ DEFAULT NOW(),
    
    PRIMARY KEY (room_id, user_id)
);

CREATE INDEX idx_read_receipts_message ON read_receipts(message_id);

CREATE TABLE IF NOT EXISTS files (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    room_id UUID REFERENCES rooms(id),
    file_name VARCHAR(255) NOT NULL,
    file_size BIGINT NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    storage_path TEXT NOT NULL,
    storage_bucket VARCHAR(100) NOT NULL,
    thumbnail_path TEXT,
    width INT,
    height INT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ
);

CREATE INDEX idx_files_room ON files(room_id);
CREATE INDEX idx_files_user ON files(user_id);
