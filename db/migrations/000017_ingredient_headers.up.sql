-- This migration updates the JSONB structure of all existing ingredients
-- from: {"name": "Flour", "quantity": "1 cup"}
-- to:   {"type": "ingredient", "name": "Flour", "quantity": "1 cup"}

-- 1. Create a helper function to perform the data migration
CREATE OR REPLACE FUNCTION migrate_ingredients_to_typed()
RETURNS void AS $$
DECLARE
rec RECORD;
new_ingredients JSONB;
BEGIN
FOR rec IN SELECT id, ingredients FROM recipes LOOP
-- This query unnests the array, adds the "type" key to each object,
-- and re-aggregates it into a new JSONB array.
SELECT jsonb_agg(
elem || '{"type": "ingredient"}'::jsonb
)
INTO new_ingredients
FROM jsonb_array_elements(rec.ingredients) AS elem;

    -- Handle recipes that had an empty '[]' ingredients list
    IF new_ingredients IS NULL THEN
        new_ingredients := '[]'::jsonb;
    END IF;

    -- Update the recipe row with the new structure
    UPDATE recipes
    SET ingredients = new_ingredients
    WHERE id = rec.id;
END LOOP;


END;
$$ LANGUAGE plpgsql;

-- 2. Run the migration function
SELECT migrate_ingredients_to_typed();

-- 3. Clean up the function
DROP FUNCTION migrate_ingredients_to_typed();