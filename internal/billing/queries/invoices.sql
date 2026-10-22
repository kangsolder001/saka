-- Invoice numbers come from one global sequence (migration 00010): never
-- reused, and not rolled back with the transaction that took one, so a failed
-- checkout leaves a gap.
-- name: NextInvoiceSequence :one
SELECT nextval('invoice_number_seq')::bigint AS seq;
