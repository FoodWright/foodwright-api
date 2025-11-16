-- 1. Remove unit_preference from users table
ALTER TABLE users
DROP COLUMN IF EXISTS unit_preference;

-- 2. Create a helper function to revert ingredient data
CREATE OR REPLACE FUNCTION unmigrate_from_structured_ingredients()
RETURNS void AS $$
DECLARE
rec RECORD;
new_ingredients JSONB;
elem JSONB;
new_elem JSONB;
rebuilt_quantity TEXT;
BEGIN
FOR rec IN SELECT id, ingredients FROM recipes LOOP
new_ingredients := '[]'::jsonb;

    FOR elem IN SELECT * FROM jsonb_array_elements(rec.ingredients)
    LOOP
        -- If it's a header, add it as-is
        IF elem->>'type' = 'header' THEN
            new_elem := elem;
        ELSE
            -- It's an ingredient. Rebuild the old "quantity" string
            rebuilt_quantity := trim(elem->>'quantity_str' || ' ' || elem->>'unit');

            -- Rebuild the old object
            new_elem := jsonb_build_object(
                'type', elem->>'type',
                'name', elem->>'name',
                'quantity', rebuilt_quantity
            );
        END IF;
        
        new_ingredients := new_ingredients || new_elem;
    END LOOP;

    -- Update the row
    UPDATE recipes
    SET ingredients = new_ingredients
    WHERE id = rec.id;
END LOOP;


END;
$$ LANGUAGE plpgsql;

-- 3. Run the migration function
SELECT unmigrate_from_structured_ingredients();

-- 4. Clean up the function
DROP FUNCTION unmigrate_from_structured_ingredients();