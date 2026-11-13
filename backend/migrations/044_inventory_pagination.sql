CREATE INDEX ix_lots_space_id ON pantry.food_lots(space_id,id) WHERE deleted_at IS NULL AND lifecycle='active';
CREATE INDEX ix_lists_page ON shopping.shopping_lists(space_id,planned_date DESC,id) WHERE deleted_at IS NULL;
