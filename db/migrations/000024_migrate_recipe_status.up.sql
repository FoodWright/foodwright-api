-- This file is executed when you "migrate up"

-- Migrate all recipes to new status values
-- Convert 'approved', 'pending', and 'rejected' to 'public'
-- Keep 'private' as is
UPDATE recipes SET status = 'public' WHERE status IN ('approved', 'pending', 'rejected');

-- Update constraint to only allow 'public' or 'private'
ALTER TABLE recipes DROP CONSTRAINT IF EXISTS recipes_status_check;
ALTER TABLE recipes ADD CONSTRAINT recipes_status_check CHECK (status IN ('public', 'private'));
