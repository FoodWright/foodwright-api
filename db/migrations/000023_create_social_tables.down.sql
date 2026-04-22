-- This file is executed when you "migrate down"

-- Remove social counters from users table
ALTER TABLE users DROP COLUMN IF EXISTS follower_count;
ALTER TABLE users DROP COLUMN IF EXISTS following_count;

-- Drop tables in reverse order (respecting foreign keys)
DROP TABLE IF EXISTS post_reposts;
DROP TABLE IF EXISTS post_likes;
DROP TABLE IF EXISTS posts;
DROP TABLE IF EXISTS user_follows;
