CREATE FUNCTION platform.track_versioned_fields() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 field record;
 changed_fields text[] := ARRAY[]::text[];
 field_name text;
BEGIN
 FOR field IN SELECT * FROM jsonb_each_text(TG_ARGV[0]::jsonb) LOOP
  IF to_jsonb(NEW)->field.value IS DISTINCT FROM to_jsonb(OLD)->field.value THEN
   changed_fields := array_append(changed_fields,field.key);
  END IF;
 END LOOP;
 NEW.version := GREATEST(NEW.version,OLD.version);
 IF cardinality(changed_fields)>0 AND NEW.version=OLD.version THEN
  NEW.version := OLD.version+1;
 END IF;
 NEW.field_versions := OLD.field_versions;
 FOREACH field_name IN ARRAY changed_fields LOOP
  NEW.field_versions := jsonb_set(NEW.field_versions,ARRAY[field_name],to_jsonb(NEW.version));
 END LOOP;
 RETURN NEW;
END;
$$;

DO $$
DECLARE spec record;
BEGIN
 FOR spec IN SELECT * FROM (VALUES
  ('identity.users','{"fullName":"full_name","locale":"locale","avatarObjectId":"avatar_object_id","email":"email","status":"status","systemRole":"system_role","lockReason":"lock_reason"}'),
  ('household.spaces','{"name":"name","avatarObjectId":"avatar_object_id","dissolvedAt":"dissolved_at"}'),
  ('catalog.foods','{"name":"name","categoryId":"category_id","defaultUnitId":"default_unit_id","status":"status","defaultLocation":"default_location","scope":"scope","ownerSpaceId":"owner_space_id","mergedInto":"merged_into"}'),
  ('pantry.food_lots','{"storageLocation":"storage_location","expiresOn":"expires_on","notes":"notes","remainingQuantity":"remaining_qty","remainingQty":"remaining_qty","lifecycle":"lifecycle","foodId":"food_id"}'),
  ('shopping.shopping_lists','{"name":"name","plannedDate":"planned_date","notes":"notes","status":"status"}'),
  ('shopping.shopping_items','{"name":"name","quantity":"quantity","actualQuantity":"actual_quantity","unitPrice":"unit_price","notes":"notes","status":"status","assigneeId":"assignee_id","purchasedBy":"purchased_by","foodId":"food_id","unitId":"unit_id"}')
 ) AS resources(table_name,field_mapping) LOOP
  EXECUTE format('ALTER TABLE %s ADD COLUMN field_versions jsonb NOT NULL DEFAULT ''{}'' CHECK(jsonb_typeof(field_versions)=''object'')',spec.table_name);
  EXECUTE format('UPDATE %s t SET field_versions=(SELECT jsonb_object_agg(key,to_jsonb(t.version)) FROM jsonb_each_text($1::jsonb))',spec.table_name) USING spec.field_mapping;
  EXECUTE format('CREATE TRIGGER track_versioned_fields BEFORE UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION platform.track_versioned_fields(%L)',spec.table_name,spec.field_mapping);
 END LOOP;
END;
$$;
