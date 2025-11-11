-- This file is executed when you "migrate up"

-- Add a new 'image_url' column to the recipes table.
-- It can be NULL because not all recipes will have an image.
ALTER TABLE recipes
ADD COLUMN IF NOT EXISTS image_url TEXT;