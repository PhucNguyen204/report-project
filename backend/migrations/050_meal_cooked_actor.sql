ALTER TABLE mealplan.meal_entries ADD COLUMN cooked_by uuid REFERENCES identity.users(id);

UPDATE mealplan.meal_entries AS meal
SET cooked_by=activity.actor_id
FROM (
 SELECT DISTINCT ON (space_id,entity_id) space_id,entity_id,actor_id
 FROM platform.activities
 WHERE kind='meal.cooked'
 ORDER BY space_id,entity_id,created_at DESC,seq DESC
) AS activity
JOIN identity.users AS actor ON actor.id=activity.actor_id
WHERE meal.space_id=activity.space_id AND meal.id::text=activity.entity_id AND meal.status='cooked';
