-- This file is executed when you "migrate up"

-- 1. Add 'rating' and 'notes' columns to the user_cooks_log table
ALTER TABLE user_cooks_log
ADD COLUMN IF NOT EXISTS rating INT CHECK (rating >= 1 AND rating <= 5), -- Rating from 1 to 5, can be NULL
ADD COLUMN IF NOT EXISTS notes TEXT; -- Free-form text notes, can be NULL

-- 2. Add an index to query logs by recipe_id and sort by creation time
-- This will be very fast for showing "latest notes" on a recipe page
CREATE INDEX IF NOT EXISTS idx_user_cooks_log_recipe_id_created_at
ON user_cooks_log(recipe_id, created_at DESC);