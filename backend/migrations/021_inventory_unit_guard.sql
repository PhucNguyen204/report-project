CREATE FUNCTION pantry.guard_unit_dimension() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.dimension IS DISTINCT FROM OLD.dimension AND
    (EXISTS(SELECT 1 FROM pantry.food_lots WHERE unit_id=OLD.id)
     OR EXISTS(SELECT 1 FROM shopping.shopping_items WHERE unit_id=OLD.id)) THEN
  RAISE EXCEPTION 'Referenced inventory unit dimension cannot change' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER inventory_unit_dimension_guard BEFORE UPDATE OF dimension ON catalog.units
 FOR EACH ROW EXECUTE FUNCTION pantry.guard_unit_dimension();
