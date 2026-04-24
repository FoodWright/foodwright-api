ALTER TABLE posts DROP CONSTRAINT IF EXISTS posts_check;

ALTER TABLE posts ADD CONSTRAINT posts_check CHECK (
    (post_type = 'cook_log' AND recipe_id IS NOT NULL AND cook_log_id IS NOT NULL AND content IS NULL) OR
    (post_type = 'recipe_share' AND recipe_id IS NOT NULL AND cook_log_id IS NULL AND content IS NULL) OR
    (post_type = 'quick_post' AND recipe_id IS NULL AND cook_log_id IS NULL AND content IS NOT NULL)
);
