ALTER TABLE recipe.recipes ADD COLUMN image_object_id uuid;
CREATE INDEX ix_recipe_live_image ON recipe.recipes(image_object_id) WHERE image_object_id IS NOT NULL AND deleted_at IS NULL;
