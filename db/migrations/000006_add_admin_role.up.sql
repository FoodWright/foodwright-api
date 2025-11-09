-- This file is executed when you "migrate up"

-- 1. Add an 'is_admin' column to the users table
ALTER TABLE users
ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT FALSE;

-- 2. Manually make YOUR user an admin
-- !! REPLACE 'YOUR_USER_ID' with your Firebase UID from the 'users' table !!
-- You can find this by logging in and checking the 'id' column in your 'users' table.
UPDATE users
SET is_admin = TRUE
WHERE id = 'l3vfGl0PiVcnICNoMlgrps9yaZn1'; -- <-- IMPORTANT: CHANGE THIS

-- 3. Add a "Recipe Smith" badge for our new logic
INSERT INTO recipes (title, description, xp, tags, status)
VALUES
('Admin Test Recipe', 'A test recipe.', 10, '{}', 'approved')
ON CONFLICT (title) DO NOTHING;