-- This file is executed when you "migrate down"

-- Revert trigger names
UPDATE badges
SET trigger_event = 'on_approval'
WHERE trigger_event = 'on_recipe_publish';

-- Delete new social badges
DELETE FROM badges WHERE name IN ('Social Butterfly', 'Popular Chef', 'Content Creator', 'Recipe Ambassador');
