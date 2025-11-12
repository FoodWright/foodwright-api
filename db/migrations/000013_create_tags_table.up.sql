-- This file is executed when you "migrate up"

-- 1. Create the new 'tags' table to store our official list
CREATE TABLE IF NOT EXISTS tags (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL UNIQUE,
    description TEXT, -- For a future "browse by tag" page
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2. Populate the table with our existing tags
-- We'll just grab all distinct tags currently in the recipes table.
INSERT INTO tags (name)
SELECT DISTINCT unnest(tags) 
FROM recipes
WHERE cardinality(tags) > 0
ON CONFLICT (name) DO NOTHING;

-- 3. Add any other tags we might want
INSERT INTO tags (name) VALUES
('quick'),
('easy'),
('dessert'),
('vegetarian'),
('vegan'),
('gluten-free'),
('dinner'),
('lunch'),
('breakfast'),
('appetizer'),
('side-dish')
ON CONFLICT (name) DO NOTHING;