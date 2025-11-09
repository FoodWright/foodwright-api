-- This file is executed when you "migrate down"

ALTER TABLE recipes
DROP COLUMN IF EXISTS ingredients,
DROP COLUMN IF EXISTS instructions;