-- This file is executed when you "migrate down"

-- 1. Add the old 'badges' column back to 'users'
ALTER TABLE users
ADD COLUMN IF NOT EXISTS badges TEXT[] NOT NULL DEFAULT '{}';

-- 2. DATA MIGRATION: Repopulate the old 'badges' array
-- This is a "reverse" migration to preserve data
UPDATE users u
SET badges = (
    SELECT array_agg(b.name)
    FROM user_badges ub
    JOIN badges b ON ub.badge_id = b.id
    WHERE ub.user_id = u.id
);

-- 3. Drop the new tables
DROP TABLE IF EXISTS user_badges;
DROP TABLE IF EXISTS badges;

-- 4. Drop the 'is_site_admin' column
ALTER TABLE users
DROP COLUMN IF EXISTS is_site_admin;