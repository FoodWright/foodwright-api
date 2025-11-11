-- This file is executed when you "migrate down"

ALTER TABLE recipes
DROP COLUMN IF EXISTS image_url;