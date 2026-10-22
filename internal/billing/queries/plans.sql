-- name: GetPlanByID :one
SELECT * FROM plans WHERE id = @id;

-- name: GetPlanByCode :one
SELECT * FROM plans WHERE code = @code;

-- The public catalogue: active plans only, in the order the plans page shows them.
-- name: ListActivePlans :many
SELECT * FROM plans WHERE is_active ORDER BY sort_order, price_amount, code;
