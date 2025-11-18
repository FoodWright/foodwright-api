-- This file is executed when you "migrate down"

ALTER TABLE recipes
DROP COLUMN IF EXISTS prep_time_minutes,
DROP COLUMN IF EXISTS cook_time_minutes,
DROP COLUMN IF EXISTS servings;