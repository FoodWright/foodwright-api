-- This file is executed when you "migrate up"

-- Create user_follows table for Twitter-style follow relationships
CREATE TABLE IF NOT EXISTS user_follows (
    follower_id VARCHAR(255) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    followed_id VARCHAR(255) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (follower_id, followed_id),
    CHECK (follower_id != followed_id)  -- Prevent self-follows
);

CREATE INDEX idx_user_follows_follower ON user_follows(follower_id);
CREATE INDEX idx_user_follows_followed ON user_follows(followed_id);

-- Create posts table for unified feed content (3 types: cook_log, recipe_share, quick_post)
CREATE TABLE IF NOT EXISTS posts (
    id BIGSERIAL PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    post_type VARCHAR(50) NOT NULL,  -- 'cook_log', 'recipe_share', 'quick_post'

    -- For cook_log and recipe_share types
    recipe_id BIGINT REFERENCES recipes(id) ON DELETE CASCADE,
    cook_log_id BIGINT REFERENCES user_cooks_log(id) ON DELETE CASCADE,

    -- For quick_post type
    content TEXT,  -- Max 500 chars, enforced in app

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Constraints to ensure proper data based on post_type
    CHECK (
        (post_type = 'cook_log' AND recipe_id IS NOT NULL AND cook_log_id IS NOT NULL AND content IS NULL) OR
        (post_type = 'recipe_share' AND recipe_id IS NOT NULL AND cook_log_id IS NULL AND content IS NULL) OR
        (post_type = 'quick_post' AND recipe_id IS NULL AND cook_log_id IS NULL AND content IS NOT NULL)
    )
);

CREATE INDEX idx_posts_user_created ON posts(user_id, created_at DESC);
CREATE INDEX idx_posts_created ON posts(created_at DESC);
CREATE INDEX idx_posts_recipe ON posts(recipe_id) WHERE recipe_id IS NOT NULL;

-- Create post_likes table for liking any post
CREATE TABLE IF NOT EXISTS post_likes (
    post_id BIGINT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    user_id VARCHAR(255) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (post_id, user_id)
);

CREATE INDEX idx_post_likes_post ON post_likes(post_id);
CREATE INDEX idx_post_likes_user ON post_likes(user_id);

-- Create post_reposts table for sharing posts to your feed
CREATE TABLE IF NOT EXISTS post_reposts (
    id BIGSERIAL PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    original_post_id BIGINT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, original_post_id)  -- Can only repost once
);

CREATE INDEX idx_post_reposts_user ON post_reposts(user_id, created_at DESC);
CREATE INDEX idx_post_reposts_post ON post_reposts(original_post_id);

-- Add social counters to users table (denormalized for performance)
ALTER TABLE users ADD COLUMN IF NOT EXISTS follower_count INT DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS following_count INT DEFAULT 0;
