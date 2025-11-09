-- This file is executed when you "migrate down"

ALTER TABLE recipes
DROP COLUMN IF EXISTS status,
DROP COLUMN IF EXISTS submitted_by_user_id;
-- Note: Dropping the column automatically drops the index