-- The Vault shop. Purchases never touch this file's money paths — the debit
-- rides the wallet ledger (kind 'shop_purchase'); these are catalog/ownership
-- reads plus the inventory grant inside the purchase transaction.

-- name: ListActiveShopItems :many
SELECT id, kind, name, blurb, price_credits, rarity, payload, sort
FROM shop_items
WHERE is_active
ORDER BY kind, sort, price_credits;

-- name: GetActiveShopItem :one
SELECT id, kind, name, blurb, price_credits, rarity, payload
FROM shop_items
WHERE id = $1 AND is_active;

-- name: ListActiveEmotePacks :many
-- The pack-gated emote set, for the server-side wheel gate: each row maps a
-- pack item id to the emote ids it unlocks. Any emote id NOT listed in an
-- active pack is free to send.
SELECT id, payload
FROM shop_items
WHERE kind = 'emote_pack' AND is_active;

-- name: InsertInventoryItem :execrows
-- The (user_id, item_id) primary key backstops double-grants; a conflicting
-- retry is a no-op so idempotency replays stay harmless.
INSERT INTO user_inventory (user_id, item_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: HasInventoryItem :one
SELECT EXISTS (
    SELECT 1 FROM user_inventory WHERE user_id = $1 AND item_id = $2
) AS owned;

-- name: ListInventory :many
SELECT item_id, acquired_at
FROM user_inventory
WHERE user_id = $1
ORDER BY acquired_at DESC, item_id;
