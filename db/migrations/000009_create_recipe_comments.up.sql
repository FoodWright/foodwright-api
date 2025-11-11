-- This file is executed when you "migrate up"

CREATE TABLE IF NOT EXISTS recipe_comments (
    id BIGSERIAL PRIMARY KEY,
    
    -- Link to the recipe. If a recipe is deleted, all its comments are deleted.
    recipe_id BIGINT NOT NULL REFERENCES recipes(id) ON DELETE CASCADE,
    
    -- Link to the user.
    -- If a user is deleted, their comments remain as "Anonymous".
    user_id VARCHAR(255) REFERENCES users(id) ON DELETE SET NULL,
    
    comment_text TEXT NOT NULL CHECK (comment_text <> ''),
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for quickly fetching all comments for a recipe
CREATE INDEX IF NOT EXISTS idx_recipe_comments_recipe_id ON recipe_comments(recipe_id, created_at DESC);

-- Index for finding all comments by a user (less common, but good to have)
CREATE INDEX IF NOT EXISTS idx_recipe_comments_user_id ON recipe_comments(user_id);