-- 1. Add unit_preference to users table
ALTER TABLE users
ADD COLUMN IF NOT EXISTS unit_preference VARCHAR(10) NOT NULL DEFAULT 'imperial';

-- 2. Create a helper function to migrate ingredient data
CREATE OR REPLACE FUNCTION migrate_to_structured_ingredients()
RETURNS void AS $$
DECLARE
rec RECORD;
new_ingredients JSONB;
elem JSONB;
new_elem JSONB;
quantity_parts TEXT[];
parsed_quantity TEXT;
parsed_unit TEXT;
BEGIN
FOR rec IN SELECT id, ingredients FROM recipes LOOP
new_ingredients := '[]'::jsonb; -- Start with a new empty array

    -- Loop over each ingredient object in the JSON array
    FOR elem IN SELECT * FROM jsonb_array_elements(rec.ingredients)
    LOOP
        -- If it's a header, just add it as-is
        IF elem->>'type' = 'header' THEN
            new_elem := elem;
        ELSE
            -- It's an ingredient. Try to parse the old 'quantity' field.
            -- This regex finds the first number-like part (e.g., "1", "1/2", "1.5", "1 1/2")
            -- and separates it from the rest (the unit).
            quantity_parts := regexp_matches(
                elem->>'quantity',
                '^\s*([0-9/.\s-]+)\s*(.*)\s*$'
            );

            IF array_length(quantity_parts, 1) = 2 THEN
                parsed_quantity := trim(quantity_parts[1]);
                parsed_unit := trim(quantity_parts[2]);
            ELSE
                -- Fallback if regex fails: quantity is blank, unit is the original string
                parsed_quantity := '';
                parsed_unit := elem->>'quantity';
            END IF;

            -- Build the new JSON object, removing the old 'quantity' field
            new_elem := (elem - 'quantity') || jsonb_build_object(
                'quantity_str', parsed_quantity,
                'unit', parsed_unit
            );
        END IF;
        
        new_ingredients := new_ingredients || new_elem; -- Append the modified element
    END LOOP;

    -- Update the row
    UPDATE recipes
    SET ingredients = new_ingredients
    WHERE id = rec.id;
END LOOP;


END;
$$ LANGUAGE plpgsql;

-- 3. Run the migration function
SELECT migrate_to_structured_ingredients();

-- 4. Clean up the function
DROP FUNCTION migrate_to_structured_ingredients();