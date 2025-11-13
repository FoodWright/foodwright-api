-- 1. Re-add the NOT NULL constraint to rule_key
ALTER TABLE badges
ALTER COLUMN rule_key SET NOT NULL;

-- 2. Drop the new columns
ALTER TABLE badges
DROP COLUMN IF EXISTS trigger_event,
DROP COLUMN IF EXISTS rule_config;