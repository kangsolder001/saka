-- Webhook plumbing (spec §4.10). UNIQUE (provider, event_id) makes a delivery
-- that arrives twice one event: the second insert matches no row.
-- name: InsertPaymentEvent :one
INSERT INTO payment_events (provider, event_id, payload, received_at)
VALUES (@provider, @event_id, @payload, @now::timestamptz)
ON CONFLICT (provider, event_id) DO NOTHING
RETURNING id;

-- The event row, locked: two deliveries of one event (or a delivery and a
-- retry) are processed one after the other, and the second sees processed_at.
-- name: LockPaymentEvent :one
SELECT * FROM payment_events WHERE provider = @provider AND event_id = @event_id FOR UPDATE;

-- Marks an event processed. note is set when it was processed but needs a
-- person to look at it (spec §4.10); NULL for a clean one.
-- name: MarkPaymentEventProcessed :exec
UPDATE payment_events
SET processed_at = @now::timestamptz, error = sqlc.narg(note)
WHERE id = @id;

-- Records why processing failed. processed_at stays NULL, so the gateway's
-- retry processes the event again.
-- name: SetPaymentEventError :exec
UPDATE payment_events SET error = @error WHERE id = @id AND processed_at IS NULL;
