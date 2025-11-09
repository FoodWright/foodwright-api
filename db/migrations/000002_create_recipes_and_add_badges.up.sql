-- This file is executed when you "migrate up"

-- 1. Create the recipes table
CREATE TABLE IF NOT EXISTS recipes (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(255) NOT NULL UNIQUE, -- <-- THIS IS THE FIX
    description TEXT,
    xp INT NOT NULL DEFAULT 10,
    tags TEXT[] NOT NULL DEFAULT '{}', -- TEXT[] is a string array
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2. Add a 'badges' column to the users table
ALTER TABLE users
ADD COLUMN IF NOT EXISTS badges TEXT[] NOT NULL DEFAULT '{}';

-- 3. Seed the recipes table with our dummy data
INSERT INTO recipes (title, description, xp, tags)
VALUES
('Simple Sourdough', 'A beginner''s guide to sourdough.', 50, '{"baking", "bread"}'),
('One-Pot Pasta', 'Easy weeknight meal.', 15, '{"pasta", "easy", "one-pot"}'),
('Classic Roast Chicken', 'The perfect Sunday roast.', 35, '{"roast", "chicken"}')
ON CONFLICT (title) DO NOTHING; -- This will now work correctly