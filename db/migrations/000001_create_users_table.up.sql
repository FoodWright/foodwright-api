-- This file is executed when you "migrate up"

CREATE TABLE IF NOT EXISTS users (
    -- This is the Firebase UID
    id VARCHAR(255) PRIMARY KEY NOT NULL,
    
    -- This can be their Google display name or a custom one
    username VARCHAR(100) NOT NULL,
    
    -- Gamification fields
    rank VARCHAR(50) NOT NULL DEFAULT 'Kitchen Novice',
    xp INT NOT NULL DEFAULT 0,
    
    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- We'll need to look up users by their Firebase ID, so an index is critical
CREATE INDEX IF NOT EXISTS idx_users_id ON users(id);