-- This file is executed when you "migrate up"

-- 1. Create the user_cooks_log table
-- This table is our "source of truth" for all completed cooks.
CREATE TABLE IF NOT EXISTS user_cooks_log (
    id BIGSERIAL PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    recipe_id BIGINT NOT NULL REFERENCES recipes(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2. Add indexes for faster queries
-- We will query this table by user_id constantly to check for badges
CREATE INDEX IF NOT EXISTS idx_user_cooks_log_user_id ON user_cooks_log(user_id);
-- We might also want to find all users who cooked a specific recipe
CREATE INDEX IF NOT EXISTS idx_user_cooks_log_recipe_id ON user_cooks_log(recipe_id);

-- 3. Add a "First Cook" badge to our seed data for testing
INSERT INTO recipes (title, description, xp, tags)
VALUES
('First Cook Test', 'An easy recipe to get your first badge.', 1, '{"easy"}')
ON CONFLICT (title) DO NOTHING;