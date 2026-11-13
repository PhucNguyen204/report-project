CREATE FUNCTION recipe.guard_referenced_unit_dimension() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.dimension <> OLD.dimension AND EXISTS(SELECT 1 FROM recipe.recipe_ingredients WHERE unit_id=OLD.id) THEN
  RAISE EXCEPTION 'Referenced recipe unit dimension cannot change' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER guard_recipe_unit_dimension BEFORE UPDATE OF dimension ON catalog.units
 FOR EACH ROW EXECUTE FUNCTION recipe.guard_referenced_unit_dimension();
