CREATE OR REPLACE FUNCTION rollback_add_type_to_instructions()
RETURNS void AS $$
DECLARE
    rec RECORD;
    new_instructions JSONB;
BEGIN
    FOR rec IN SELECT id, instructions FROM recipes LOOP
        SELECT jsonb_agg(
            elem - 'type'
        )
        INTO new_instructions
        FROM jsonb_array_elements(rec.instructions) AS elem;

        IF new_instructions IS NULL THEN
            new_instructions := '[]'::jsonb;
        END IF;

        UPDATE recipes
        SET instructions = new_instructions
        WHERE id = rec.id;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

SELECT rollback_add_type_to_instructions();
DROP FUNCTION rollback_add_type_to_instructions();
