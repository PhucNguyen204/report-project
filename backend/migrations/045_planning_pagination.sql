CREATE INDEX ix_recipes_space_page ON recipe.recipes(space_id,name,id) WHERE deleted_at IS NULL AND status='published';
CREATE INDEX ix_recipe_templates_page ON recipe.recipes(name,id) WHERE kind='template' AND deleted_at IS NULL;
CREATE INDEX ix_meals_space_page ON mealplan.meal_entries(space_id,meal_date,meal_slot,id) WHERE deleted_at IS NULL;
