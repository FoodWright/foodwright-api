-- This file is executed when you "migrate up"

-- 1. Add the is_featured column
-- We default to FALSE so no existing recipes are featured
ALTER TABLE recipes
ADD COLUMN IF NOT EXISTS is_featured BOOLEAN NOT NULL DEFAULT FALSE;

-- 2. Create an index for fast lookups
-- We'll query for 'is_featured = TRUE' and 'status = 'approved'
CREATE INDEX IF NOT EXISTS idx_recipes_featured_approved
ON recipes(is_featured, status)
WHERE is_featured = TRUE AND status = 'approved';