CREATE OR REPLACE FUNCTION rollback_remove_order_fields()
RETURNS void AS $$
DECLARE
    rec RECORD;
    new_ingredients JSONB;
    new_instructions JSONB;
BEGIN
    FOR rec IN SELECT id, ingredients, instructions FROM recipes LOOP
        -- Process Ingredients: remove 'order' key
        SELECT jsonb_agg(
            elem - 'order'
        )
        INTO new_ingredients
        FROM jsonb_array_elements(rec.ingredients) AS elem;

        IF new_ingredients IS NULL THEN
            new_ingredients := '[]'::jsonb;
        END IF;

        -- Process Instructions: remove 'order' key
        SELECT jsonb_agg(
            elem - 'order'
        )
        INTO new_instructions
        FROM jsonb_array_elements(rec.instructions) AS elem;

        IF new_instructions IS NULL THEN
            new_instructions := '[]'::jsonb;
        END IF;

        UPDATE recipes
        SET ingredients = new_ingredients,
            instructions = new_instructions
        WHERE id = rec.id;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

SELECT rollback_remove_order_fields();

DROP FUNCTION rollback_remove_order_fields();
