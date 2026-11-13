// Package redis holds the Redis-backed stores used by the auth layer.
//
// It provides:
//
//   - RevocationStore: records revoked sessions and per-user "valid after"
//     timestamps so access tokens can be rejected before they expire. Entries
//     live for the access-token TTL, after which the token is invalid anyway.
//   - LoginLimiter: throttles login attempts per email, allowing a number of
//     free attempts and then applying an increasing backoff.
//   - MailRequestLimiter: limits how often emails (verification, password
//     reset, ...) can be requested for the same address.
//
// Limiter keys use a hash of the email address, so raw addresses are never
// stored in Redis. All types share a *redis.Client (go-redis) owned by the
// caller; this package never opens or closes connections.
package redis
