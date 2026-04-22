-- This file is executed when you "migrate down"

-- Delete backfilled posts (identifiable by cook_log_id)
DELETE FROM posts WHERE post_type = 'cook_log';
