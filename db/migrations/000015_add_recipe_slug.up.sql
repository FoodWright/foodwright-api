-- 1. Add the new 'slug' column
ALTER TABLE recipes
ADD COLUMN IF NOT EXISTS slug VARCHAR(255);

-- 2. Create a function to generate slugs
-- This creates a helper function inside Postgres to do the string cleanup
CREATE OR REPLACE FUNCTION slugify(text)
RETURNS text AS $$
  SELECT lower(
    regexp_replace(
      -- Remove all non-alphanumeric characters
      regexp_replace($1, '[^a-zA-Z0-9\s-]+', '', 'g'),
      -- Replace spaces and repeated hyphens with a single hyphen
      '[\s-]+', '-', 'g'
    )
  )
$$ LANGUAGE sql IMMUTABLE;

-- 3. Backfill the slug for all existing recipes
UPDATE recipes
SET slug = slugify(title)
WHERE slug IS NULL;

-- 4. Make the column NOT NULL after backfilling
ALTER TABLE recipes
ALTER COLUMN slug SET NOT NULL;

-- 5. Add an index (optional but good practice)
CREATE INDEX IF NOT EXISTS idx_recipes_slug ON recipes(slug);