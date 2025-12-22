CREATE OR REPLACE FUNCTION migrate_add_order_fields()
RETURNS void AS $$
DECLARE
    rec RECORD;
    new_ingredients JSONB;
    new_instructions JSONB;
BEGIN
    FOR rec IN SELECT id, ingredients, instructions FROM recipes LOOP
        -- Process Ingredients
        -- We use idx - 1 to make it 0-indexed
        SELECT jsonb_agg(
            elem || jsonb_build_object('order', idx - 1)
        )
        INTO new_ingredients
        FROM jsonb_array_elements(rec.ingredients) WITH ORDINALITY AS t(elem, idx);

        IF new_ingredients IS NULL THEN
            new_ingredients := '[]'::jsonb;
        END IF;

        -- Process Instructions
        SELECT jsonb_agg(
            elem || jsonb_build_object('order', idx - 1)
        )
        INTO new_instructions
        FROM jsonb_array_elements(rec.instructions) WITH ORDINALITY AS t(elem, idx);

        IF new_instructions IS NULL THEN
            new_instructions := '[]'::jsonb;
        END IF;

        -- Update the recipe
        UPDATE recipes
        SET ingredients = new_ingredients,
            instructions = new_instructions
        WHERE id = rec.id;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

SELECT migrate_add_order_fields();

DROP FUNCTION migrate_add_order_fields();
