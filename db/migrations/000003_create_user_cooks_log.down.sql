-- This file is executed when you "migrate down"

DROP TABLE IF EXISTS user_cooks_log;

-- Note: We don't remove the recipe we inserted in the 'up' file
-- as it's harmless, but in a production system you might.