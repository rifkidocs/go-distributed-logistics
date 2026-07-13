-- name: CreateShipment :one
INSERT INTO shipments (order_id, item_id, quantity, destination, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, order_id, item_id, quantity, destination, status, carrier, tracking_number, created_at, updated_at;

-- name: GetShipment :one
SELECT id, order_id, item_id, quantity, destination, status, carrier, tracking_number, created_at, updated_at
FROM shipments
WHERE id = $1;

-- name: GetShipmentByOrder :one
SELECT id, order_id, item_id, quantity, destination, status, carrier, tracking_number, created_at, updated_at
FROM shipments
WHERE order_id = $1;

-- name: UpdateShipmentStatus :one
UPDATE shipments
SET status = $2, updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING id, order_id, item_id, quantity, destination, status, carrier, tracking_number, created_at, updated_at;

-- name: AssignCarrier :one
UPDATE shipments
SET carrier = $2, tracking_number = $3, status = 'ASSIGNED', updated_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING id, order_id, item_id, quantity, destination, status, carrier, tracking_number, created_at, updated_at;
