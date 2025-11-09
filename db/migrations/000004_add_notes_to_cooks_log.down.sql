-- This file is executed when you "migrate down"

ALTER TABLE user_cooks_log
DROP COLUMN IF EXISTS rating,
DROP COLUMN IF EXISTS notes;

-- Drop the index we created
DROP INDEX IF EXISTS idx_user_cooks_log_recipe_id_created_at;