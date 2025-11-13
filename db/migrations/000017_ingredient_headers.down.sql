-- This migration reverts the change by removing the "type" key from all ingredients

-- 1. Create a helper function to revert the data
CREATE OR REPLACE FUNCTION unmigrate_ingredients_to_typed()
RETURNS void AS $$
DECLARE
rec RECORD;
new_ingredients JSONB;
BEGIN
FOR rec IN SELECT id, ingredients FROM recipes LOOP
-- This query unnests the array, removes the "type" key,
-- and re-aggregates it.
SELECT jsonb_agg(
elem - 'type'
)
INTO new_ingredients
FROM jsonb_array_elements(rec.ingredients) AS elem;

    -- Handle recipes that had an empty '[]' ingredients list
    IF new_ingredients IS NULL THEN
        new_ingredients := '[]'::jsonb;
    END IF;

    -- Update the recipe row with the old structure
    UPDATE recipes
    SET ingredients = new_ingredients
    WHERE id = rec.id;
END LOOP;


END;
$$ LANGUAGE plpgsql;

-- 2. Run the migration function
SELECT unmigrate_ingredients_to_typed();

-- 3. Clean up the function
DROP FUNCTION unmigrate_ingredients_to_typed();