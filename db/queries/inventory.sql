-- name: CreateUser :one
INSERT INTO users (username, password_hash, role)
VALUES ($1, $2, $3)
RETURNING id, username, role, created_at;

-- name: GetUserByUsername :one
SELECT id, username, password_hash, role, created_at
FROM users
WHERE username = $1;

-- name: CreateItem :one
INSERT INTO items (id, name, sku, description, price)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, sku, description, price, created_at;

-- name: GetItem :one
SELECT id, name, sku, description, price, created_at
FROM items
WHERE id = $1;

-- name: GetStock :one
SELECT quantity, updated_at
FROM stock_levels
WHERE warehouse_id = $1 AND item_id = $2;

-- name: UpsertStock :one
INSERT INTO stock_levels (warehouse_id, item_id, quantity, updated_at)
VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
ON CONFLICT (warehouse_id, item_id)
DO UPDATE SET quantity = stock_levels.quantity + EXCLUDED.quantity, updated_at = CURRENT_TIMESTAMP
RETURNING warehouse_id, item_id, quantity, updated_at;

-- name: SetStock :one
INSERT INTO stock_levels (warehouse_id, item_id, quantity, updated_at)
VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
ON CONFLICT (warehouse_id, item_id)
DO UPDATE SET quantity = EXCLUDED.quantity, updated_at = CURRENT_TIMESTAMP
RETURNING warehouse_id, item_id, quantity, updated_at;

-- name: CreateLedgerEntry :one
INSERT INTO stock_ledger (item_id, warehouse_id, quantity_change, transaction_type, order_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, item_id, warehouse_id, quantity_change, transaction_type, order_id, created_at;
