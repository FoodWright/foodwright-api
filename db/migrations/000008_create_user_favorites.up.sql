-- This file is executed when you "migrate up"

-- This table creates a many-to-many relationship between
-- users and recipes.
CREATE TABLE IF NOT EXISTS user_favorites (
    user_id VARCHAR(255) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipe_id BIGINT NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- A user can only favorite a recipe once
    PRIMARY KEY (user_id, recipe_id)
);

-- We'll need to quickly find all favorites for a user
CREATE INDEX IF NOT EXISTS idx_user_favorites_user_id ON user_favorites(user_id);