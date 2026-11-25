// Package metrics defines the Prometheus metrics of the API and the small
// types that record them.
//
// Each group of metrics is a struct built by a constructor that registers its
// collectors on the [prometheus.Registry] it is given, so the caller decides
// which registry is exposed (the ops server serves it at /metrics):
//
//   - [HTTP] ([NewHTTP]): http_requests_total, http_request_duration_seconds
//     (both labelled by method, chi route pattern and status) and the
//     http_requests_in_flight gauge.
//   - [Auth] ([NewAuth]): auth_revocation_check_duration_seconds, the latency
//     of the Redis revocation check on authenticated requests;
//     auth_revocation_fail_open_total, requests let through because that check
//     failed, labelled by reason ([FailOpenTimeout] or [FailOpenError]); and
//     auth_logins_total, login attempts labelled by result.
//   - [Task] ([NewTask]): task_authz_denied_total, task_invitations_total and
//     task_limiter_fail_open_total, invitation limiter calls that failed and were
//     let through, labelled by limiter ([LimiterInviteMail] or [LimiterInviteOwner]).
//
// The collectors are registered once, when the constructor runs. Calling a
// constructor twice with the same registry panics, so build each group once
// at startup and share it. Label values should come from a small fixed set
// (route patterns, not raw paths) to keep the cardinality bounded.
package metrics
