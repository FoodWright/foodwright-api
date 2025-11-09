-- This file is executed when you "migrate up"

-- 1. Add a status column to recipes
-- This will be 'pending', 'approved', 'rejected'
ALTER TABLE recipes
ADD COLUMN IF NOT EXISTS status VARCHAR(50) NOT NULL DEFAULT 'pending';

-- 2. Add a column to track who submitted the recipe
-- It's a foreign key to the users table.
-- We make it NULLABLE because our original recipes weren't submitted by a user.
-- ON DELETE SET NULL: If a user is deleted, their recipes become "orphaned" (anonymous)
-- but are not deleted, which is good for site content.
ALTER TABLE recipes
ADD COLUMN IF NOT EXISTS submitted_by_user_id VARCHAR(255) REFERENCES users(id) ON DELETE SET NULL;

-- 3. Create an index for querying a user's submissions
CREATE INDEX IF NOT EXISTS idx_recipes_submitted_by_user_id ON recipes(submitted_by_user_id);

-- 4. CRITICAL: Update all our existing, official recipes to 'approved'
-- Otherwise they will disappear from the site!
UPDATE recipes
SET status = 'approved'
WHERE submitted_by_user_id IS NULL; -- Affects all our seed data