-- This file is executed when you "migrate up"

-- 1. Add the new 'is_site_admin' column for the site owner
ALTER TABLE users
ADD COLUMN IF NOT EXISTS is_site_admin BOOLEAN NOT NULL DEFAULT FALSE;

-- 2. Manually make YOUR user the first Site Admin
-- !! REPLACE 'YOUR_USER_ID' with your Firebase UID from the 'users' table !!
UPDATE users
SET is_site_admin = TRUE
WHERE id = 'l3vfGl0PiVcnICNoMlgrps9yaZn1'; -- <-- IMPORTANT: CHANGE THIS

-- 3. Create the new 'badges' table to define all badges
-- This includes your 'icon_url' request!
CREATE TABLE IF NOT EXISTS badges (
    id BIGSERIAL PRIMARY KEY,
    rule_key VARCHAR(100) NOT NULL UNIQUE, -- e.g., "COOK_1", "BAKER_3"
    name VARCHAR(100) NOT NULL,
    description TEXT NOT NULL,
    icon_url TEXT, -- The image/icon for the badge
    badge_type VARCHAR(50) NOT NULL DEFAULT 'MILESTONE', -- 'MILESTONE', 'EVENT', 'SEASONAL'
    
    -- For seasonal/event badges
    start_date TIMESTAMPTZ,
    end_date TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 4. Create the 'user_badges' join table
CREATE TABLE IF NOT EXISTS user_badges (
    user_id VARCHAR(255) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    badge_id BIGINT NOT NULL REFERENCES badges(id) ON DELETE CASCADE,
    earned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- A user can only earn each badge once
    PRIMARY KEY (user_id, badge_id)
);
CREATE INDEX IF NOT EXISTS idx_user_badges_user_id ON user_badges(user_id);
CREATE INDEX IF NOT EXISTS idx_user_badges_badge_id ON user_badges(badge_id);


-- 5. Populate the 'badges' table with our existing badges
-- We'll use Font Awesome icon names as placeholders
INSERT INTO badges (rule_key, name, description, icon_url)
VALUES
('FIRST_COOK', 'First Cook', 'Log your very first recipe.', 'fas fa-utensil-spoon'),
('BAKER_3', 'Baker', 'Log 3 different recipes with the "baking" tag.', 'fas fa-birthday-cake'),
('RECIPE_SMITH_1', 'Recipe Smith', 'Submit a recipe that gets approved by the Guild.', 'fas fa-file-signature')
ON CONFLICT (rule_key) DO NOTHING;


-- 6. DATA MIGRATION: Move data from old `users.badges` array to new `user_badges` table
-- This is the most complex step. It finds every user that has a specific badge
-- in their array, and inserts a row for it in the new table.

INSERT INTO user_badges (user_id, badge_id)
SELECT u.id, b.id
FROM users u, badges b
WHERE b.name = 'First Cook' AND u.badges @> ARRAY['First Cook']
ON CONFLICT (user_id, badge_id) DO NOTHING;

INSERT INTO user_badges (user_id, badge_id)
SELECT u.id, b.id
FROM users u, badges b
WHERE b.name = 'Baker' AND u.badges @> ARRAY['Baker']
ON CONFLICT (user_id, badge_id) DO NOTHING;

INSERT INTO user_badges (user_id, badge_id)
SELECT u.id, b.id
FROM users u, badges b
WHERE b.name = 'Recipe Smith' AND u.badges @> ARRAY['Recipe Smith']
ON CONFLICT (user_id, badge_id) DO NOTHING;


-- 7. Finally, drop the old 'badges' column from the users table
ALTER TABLE users
DROP COLUMN IF EXISTS badges;