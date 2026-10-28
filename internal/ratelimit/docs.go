// Package ratelimit limits how often a client may make requests.
//
// A limiter works on string keys (for example a client IP) and is a token
// bucket per key: it holds up to burst tokens and refills at limit tokens per
// second. Allow spends one token, or reports how long the caller should wait
// before retrying. It satisfies the limiter interface used by the httpapi
// rate-limit middleware, which turns a refusal into a 429 with a Retry-After
// header.
//
// [MemoryLimiter] keeps the buckets in process memory, so limits are not shared
// between server instances and are lost on restart. Run evicts keys that have
// been idle for a while and should be started in its own goroutine.
package ratelimit
