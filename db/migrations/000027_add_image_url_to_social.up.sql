-- Add image_url to posts and user_cooks_log
ALTER TABLE posts ADD COLUMN image_url TEXT;
ALTER TABLE user_cooks_log ADD COLUMN image_url TEXT;
