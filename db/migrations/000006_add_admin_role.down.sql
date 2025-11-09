-- This file is executed when you "migrate down"

ALTER TABLE users
DROP COLUMN IF EXISTS is_admin;