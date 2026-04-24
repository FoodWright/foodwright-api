-- This file is executed when you "migrate down"

-- Revert to old status values (all public → approved)
UPDATE recipes SET status = 'approved' WHERE status = 'public';

-- Restore old constraint
ALTER TABLE recipes DROP CONSTRAINT IF EXISTS recipes_status_check;
ALTER TABLE recipes ADD CONSTRAINT recipes_status_check
  CHECK (status IN ('pending', 'approved', 'rejected', 'private'));
