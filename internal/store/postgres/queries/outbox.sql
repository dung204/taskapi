-- name: CreateOutboxItem :exec
INSERT INTO email_outbox
  (id, user_id, kind, to_email, attempts, next_attempt_at,
  locked_until, sent_at, failed_at, last_error, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: ClaimOutboxItems :many
UPDATE email_outbox
SET locked_until = sqlc.arg('locked_until'),
    attempts = attempts + 1
WHERE id IN (
  SELECT id FROM email_outbox AS o
  WHERE o.sent_at IS NULL AND o.failed_at IS NULL
    AND o.next_attempt_at <= sqlc.arg('now')
    AND (o.locked_until IS NULL OR o.locked_until < sqlc.arg('now'))
  ORDER BY o.next_attempt_at
  LIMIT sqlc.arg('limit')
  FOR UPDATE SKIP LOCKED
)
RETURNING id, user_id, kind, to_email, attempts;

-- name: DeleteSentOutboxItems :execrows
DELETE FROM email_outbox
WHERE id IN (
  SELECT id FROM email_outbox AS o
  WHERE o.sent_at < sqlc.arg('before')
  LIMIT sqlc.arg('limit')
);

-- name: MarkOutboxItemSent :exec
UPDATE email_outbox
SET sent_at = $1,
    locked_until = NULL
WHERE id = $2
  AND sent_at IS NULL;

-- name: MarkOutboxItemFailed :exec
UPDATE email_outbox
SET failed_at = $1,
    last_error = $2,
    locked_until = NULL
WHERE id = $3
  AND sent_at IS NULL
  AND failed_at IS NULL;

-- name: RescheduleOutboxItem :exec
UPDATE email_outbox
SET next_attempt_at = $1,
    last_error = $2,
    locked_until = NULL
WHERE id = $3
  AND sent_at IS NULL;
