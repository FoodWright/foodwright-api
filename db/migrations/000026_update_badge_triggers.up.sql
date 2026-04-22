-- This file is executed when you "migrate up"

-- Migrate approval badges to publish badges
UPDATE badges
SET trigger_event = 'on_recipe_publish'
WHERE trigger_event = 'on_approval';

-- Create new social badges
INSERT INTO badges (name, description, badge_type, trigger_event, rule_config, icon_url)
VALUES
  ('Social Butterfly', 'Gain 10 followers', 'MILESTONE', 'on_follow_gained',
   '{"rule_type": "TOTAL_FOLLOWERS", "threshold": 10}', NULL),
  ('Popular Chef', 'Gain 50 followers', 'MILESTONE', 'on_follow_gained',
   '{"rule_type": "TOTAL_FOLLOWERS", "threshold": 50}', NULL),
  ('Content Creator', 'Create 50 quick posts', 'MILESTONE', 'on_quick_post',
   '{"rule_type": "TOTAL_QUICK_POSTS", "threshold": 50}', NULL),
  ('Recipe Ambassador', 'Share 25 recipes to feed', 'MILESTONE', 'on_recipe_share',
   '{"rule_type": "TOTAL_RECIPE_SHARES", "threshold": 25}', NULL);
