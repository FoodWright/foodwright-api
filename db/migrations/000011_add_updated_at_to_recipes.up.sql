-- This file is executed when you "migrate up"

-- 1. Add the new 'updated_at' column
ALTER TABLE recipes
ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ;

-- 2. Backfill the new column for existing recipes
-- We'll set their 'updated_at' to their 'created_at' time
UPDATE recipes
SET updated_at = created_at
WHERE updated_at IS NULL;

-- 3. Now that it's backfilled, make it non-nullable and set default
-- (FIX: Split into two commands)
ALTER TABLE recipes
ALTER COLUMN updated_at SET NOT NULL;

ALTER TABLE recipes
ALTER COLUMN updated_at SET DEFAULT NOW();