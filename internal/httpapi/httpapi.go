// Package httpapi is the HTTP layer: it turns requests into calls on the
// domain services ([task.Store] and [auth.Service]) and their results into JSON
// responses. It knows nothing about storage; handlers only see interfaces.
//
// # Contract first
//
// The API is defined by docs/openapi.yml. oapi-codegen turns it into the chi
// routes, the request and response types and the [oapigen.StrictServerInterface]
// that this package implements (see oapigen; its generated file must not be
// edited). To change the API, edit the spec, run go generate ./..., then
// implement the new methods. [NewHandler] serves the spec at /openapi.yaml and a
// browsable UI at /docs.
//
// The generated methods are implemented by two handlers, combined in one server:
//
//   - taskHandler (task.go): task CRUD, plus validation of list filters.
//   - authHandler (auth.go): registration, email verification, login, refresh,
//     logout, password reset and change, and GET /me.
//
// # Request pipeline
//
// [NewHandler] builds the chi router from a [HandlerDeps]. A request passes
// through, from the outside in:
//
//  1. Router-wide middlewares (r.Use), which run before chi matches a route:
//     CORS (only when origins are configured, and first so preflights are
//     answered before anything else), request ID, user agent, metrics (when
//     configured), logging, and panic recovery.
//  2. The generated wrapper, which binds path and query parameters.
//  3. Per-route middlewares ([oapigen.ChiServerOptions].Middlewares), outermost
//     first: the per-IP rate limiter (see [Limiter]), then the access-token
//     check. They are per route, not in r.Use, so that a rejected request is
//     still counted with its route in the metrics instead of "unknown".
//  4. The strict handler, which decodes the body and calls the handler method.
//
// The wrapper binds parameters before step 3, so a bad parameter would be
// answered with 400 before the limiter and the token check ran. [NewHandler]
// therefore runs the same per-route chain around the parameter-error response:
// a request that is not allowed in gets 429 or 401, never a hint about what is
// wrong with its parameters.
//
// # Authentication
//
// Routes the spec marks as secured need Authorization: Bearer <access token>.
// The auth middleware verifies the JWT and asks the revocation checker whether
// its session, or the whole user, was revoked. Any failure is the same 401 with
// WWW-Authenticate: Bearer, so the answer never says why. If the revocation
// store itself fails, the request is let through (fail open) and the failure is
// logged and counted. The verified [Identity] reaches handlers through the
// context; read it with identityFrom. Public routes (security: [] in the spec)
// pass through untouched.
//
// Refresh tokens are opaque and travel either in the response body or in an
// HttpOnly cookie, chosen at login. In cookie mode the Origin header, when
// present, must be allowed by the [OriginPolicy].
//
// # Errors
//
// Every error response is {"error": "<message>"}. Handlers return expected
// outcomes as the typed response objects of the spec (a 403 for a disabled
// account, a 429 with Retry-After, ...) and everything else as an error.
// handleResponseError maps the sentinel errors of the apperr package to a status
// code: invalid input to 400, unauthorized to 401, forbidden to 403, not found
// to 404 and unavailable (or a timed-out context) to 503. A cancelled context
// writes nothing, and any other error becomes a 500 whose details are only
// logged, never sent to the client.
//
// Converters in oapigen must wrap [apperr.ErrInvalidInput] so that bad input is
// reported as 400.
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/httpapi/docs"
	"github.com/dung204/taskapi/internal/httpapi/oapigen"
	"github.com/dung204/taskapi/internal/metrics"
	"github.com/dung204/taskapi/internal/task"
)

type server struct {
	*taskHandler
	*authHandler
}

var _ oapigen.StrictServerInterface = (*server)(nil)

type Metrics struct {
	HTTP *metrics.HTTP // nillable
	Auth *metrics.Auth // nillable
	Task *metrics.Task // nillable
}

type HandlerDeps struct {
	Tasks    *task.Service
	Auth     *auth.Service
	Verifier tokenVerifier // verifies access tokens
	Revoker  revocationChecker
	Logger   *slog.Logger
	Metrics  Metrics // zero value = no metrics
	Limiter  Limiter // nillable: no rate limiting
	CORS     CORSMiddlewareOpts
	Origins  *OriginPolicy
	Now      func() time.Time // nillable, defaults to time.Now
}

func NewHandler(deps HandlerDeps) http.Handler {
	srv := &server{
		taskHandler: newTaskHandler(deps.Tasks, deps.Logger, deps.Now),
		authHandler: newAuthHandler(deps.Auth, deps.Origins, deps.Logger, deps.Now),
	}

	r := chi.NewRouter()

	mws := make([]middleware, 0)
	if len(deps.CORS.AllowedOrigins) > 0 {
		mws = append(mws, corsMiddleware(deps.CORS))
	}
	mws = append(mws, requestIDMiddleware, userAgentMiddleware)
	if deps.Metrics.HTTP != nil {
		mws = append(mws, metricsMiddleware(deps.Metrics.HTTP))
	}
	mws = append(mws, loggingMiddleware(deps.Logger), recoverMiddleware(deps.Logger))
	r.Use(mws...)

	// Per-route, not in r.Use: those run before chi matches the route, so a
	// rejected request would reach the metrics with the route label "unknown".
	//
	// The generated wrapper applies these in a loop, so the LAST entry is the
	// outermost and runs first: the limiter must come last, so that it
	// throttles requests before they cost a token verify and a revocation check.
	routeMws := []oapigen.MiddlewareFunc{
		oapigen.MiddlewareFunc(authMiddleware(AuthMiddlewareDeps{
			Checker:  deps.Revoker,
			Verifier: deps.Verifier,
			Logger:   deps.Logger,
			Metrics:  deps.Metrics.Auth,
			Now:      deps.Now,
		})),
	}
	if deps.Limiter != nil {
		routeMws = append(routeMws, oapigen.MiddlewareFunc(rateLimit(deps.Limiter, byIP)))
	}

	strict := oapigen.NewStrictHandlerWithOptions(srv, nil, oapigen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  srv.handleRequestError,
		ResponseErrorHandlerFunc: srv.handleResponseError(deps.Logger),
	})
	// The generated wrapper binds path and query parameters BEFORE it applies
	// routeMws, which only wrap the final handler call. A bad parameter would
	// therefore be answered with 400 before the rate limiter and the auth check
	// ran. Run the same chain, in the same order, around the error response, so
	// a request that is not allowed in gets 429 or 401 instead of a hint about
	// what is wrong with its parameters. Public routes are unaffected: the auth
	// middleware only acts on routes the wrapper marked as secured.
	paramErrorHandler := func(w http.ResponseWriter, r *http.Request, err error) {
		var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			srv.handleParamError(w, r, err)
		})
		for _, mw := range routeMws {
			h = mw(h)
		}
		h.ServeHTTP(w, r)
	}

	oapigen.HandlerWithOptions(strict, oapigen.ChiServerOptions{
		BaseRouter:       r,
		Middlewares:      routeMws,
		ErrorHandlerFunc: paramErrorHandler,
	})

	r.Get("/openapi.yaml", docs.Spec)
	r.Method(http.MethodGet, "/docs", http.RedirectHandler("/docs/", http.StatusMovedPermanently))
	r.Method(http.MethodGet, "/docs/*", http.StripPrefix("/docs/", docs.UI()))

	return r
}
