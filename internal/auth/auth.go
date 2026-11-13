// Package auth is the authentication domain: users, password hashing, refresh
// sessions, and the signed access tokens (JWT) and one-time user tokens
// (email verification, password reset) built on them.
//
// It owns the business rules and validation for these entities and defines the
// [SessionStore], [UserStore] and [UserTokenStore] interfaces that storage
// backends implement. It knows nothing about HTTP or SQL, so it never decides
// response status codes: its sentinel errors (such as [ErrSessionNotFound]) are
// for the package's own orchestration, and the HTTP layer maps errors to
// statuses.
package auth
