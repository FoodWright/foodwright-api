-- This file is executed when you "migrate up"

ALTER TABLE recipes
ADD COLUMN IF NOT EXISTS prep_time_minutes INT,
ADD COLUMN IF NOT EXISTS cook_time_minutes INT,
ADD COLUMN IF NOT EXISTS servings VARCHAR(100);