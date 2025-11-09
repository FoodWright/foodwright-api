-- This file is executed when you "migrate down"

-- 1. Remove the 'badges' column from users
ALTER TABLE users
DROP COLUMN IF EXISTS badges;

-- 2. Drop the recipes table
DROP TABLE IF EXISTS recipes;