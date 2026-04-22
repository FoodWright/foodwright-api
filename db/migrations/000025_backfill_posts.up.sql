-- This file is executed when you "migrate up"

-- Create posts for all existing cook logs
-- This ensures existing cook history appears in feeds
INSERT INTO posts (user_id, post_type, recipe_id, cook_log_id, created_at)
SELECT cl.user_id, 'cook_log', cl.recipe_id, cl.id, cl.created_at
FROM user_cooks_log cl
JOIN recipes r ON cl.recipe_id = r.id
WHERE r.status = 'public';
