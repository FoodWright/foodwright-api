-- This file is executed when you "migrate up"

-- 1. Add 'ingredients' and 'instructions' columns as JSONB
-- We default to an empty JSON array '[]'
ALTER TABLE recipes
ADD COLUMN IF NOT EXISTS ingredients JSONB NOT NULL DEFAULT '[]'::jsonb,
ADD COLUMN IF NOT EXISTS instructions JSONB NOT NULL DEFAULT '[]'::jsonb;

-- 2. Let's add some sample data to our existing recipes so they look good
-- We'll use a simple schema:
-- ingredients: [{"quantity": "1 cup", "name": "Flour"}]
-- instructions: [{"step": "Mix the flour and water."}]

UPDATE recipes
SET 
    ingredients = '[
        {"quantity": "500g", "name": "Strong Bread Flour"},
        {"quantity": "10g", "name": "Salt"},
        {"quantity": "350g", "name": "Warm Water"},
        {"quantity": "100g", "name": "Active Sourdough Starter"}
    ]',
    instructions = '[
        {"step": "Mix flour, water, and starter. Let rest for 30 mins (autolyse)."},
        {"step": "Add salt and mix until combined."},
        {"step": "Perform 4 sets of stretch-and-folds, 30 minutes apart."},
        {"step": "Bulk ferment for 4-6 hours until risen and bubbly."},
        {"step": "Shape and place in banneton. Proof in fridge overnight."},
        {"step": "Preheat oven and Dutch oven to 500°F (260°C)."},
        {"step": "Bake covered for 20 minutes, then uncovered for 20-25 minutes."}
    ]'
WHERE title = 'Simple Sourdough';

UPDATE recipes
SET 
    ingredients = '[
        {"quantity": "400g", "name": "Pasta (e.g., Penne)"},
        {"quantity": "1 can (28oz)", "name": "Crushed Tomatoes"},
        {"quantity": "1", "name": "Onion, chopped"},
        {"quantity": "2 cloves", "name": "Garlic, minced"},
        {"quantity": "4 cups", "name": "Vegetable Broth"},
        {"quantity": "1 tsp", "name": "Dried Oregano"}
    ]',
    instructions = '[
        {"step": "Add all ingredients to a large pot or Dutch oven."},
        {"step": "Stir to combine."},
        {"step": "Bring to a boil, then reduce heat and simmer, stirring occasionally."},
        {"step": "Cook for 10-12 minutes, or until pasta is al dente and sauce has thickened."},
        {"step": "Season with salt and pepper to taste. Serve immediately."}
    ]'
WHERE title = 'One-Pot Pasta';