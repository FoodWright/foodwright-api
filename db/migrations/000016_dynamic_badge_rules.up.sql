-- 1. Add the new columns for the rule engine
ALTER TABLE badges
ADD COLUMN IF NOT EXISTS trigger_event VARCHAR(50) NOT NULL DEFAULT 'on_cook',
ADD COLUMN IF NOT EXISTS rule_config JSONB;

-- 2. DATA MIGRATION: Convert old hard-coded rule_keys into new data-driven rules
UPDATE badges
SET
trigger_event = 'on_cook',
rule_config = '{"type": "TOTAL_COOKS", "operator": "==", "value": 1}'
WHERE rule_key = 'FIRST_COOK' AND rule_config IS NULL;

UPDATE badges
SET
trigger_event = 'on_cook',
rule_config = '{"type": "COOKS_WITH_TAG", "operator": "==", "value": 3, "parameter": "baking"}'
WHERE rule_key = 'BAKER_3' AND rule_config IS NULL;

UPDATE badges
SET
trigger_event = 'on_approval',
rule_config = '{"type": "APPROVED_SUBMISSIONS", "operator": "==", "value": 1}'
WHERE rule_key = 'RECIPE_SMITH_1' AND rule_config IS NULL;

-- 3. Now that data is migrated, make the rule_config column required
ALTER TABLE badges
ALTER COLUMN rule_config SET NOT NULL;

-- 4. Deprecate the old rule_key. We'll keep it for reference but make it optional.
ALTER TABLE badges
ALTER COLUMN rule_key DROP NOT NULL;