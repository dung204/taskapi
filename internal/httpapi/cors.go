package httpapi

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

type CORSMiddlewareOpts struct {
	AllowedOrigins []string
	MaxAge         time.Duration
}

func corsMiddleware(opts CORSMiddlewareOpts) middleware {
	allowedMethods := strings.Join([]string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete}, ", ")
	// TODO: "Authorization" is not used yet; keep it only if auth is added, otherwise remove it.
	allowedRequestHeaders := strings.Join([]string{"Content-Type", "X-Request-ID", "Authorization"}, ", ")
	allowedResponseHeaders := strings.Join([]string{"X-Request-ID", "Retry-After"}, ", ")

	anyOrigin := slices.Equal(opts.AllowedOrigins, []string{"*"})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allowed := origin != "" && (anyOrigin || slices.Contains(opts.AllowedOrigins, origin))

			if origin != "" {
				w.Header().Add("Vary", "Origin")
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}

			if r.Method == http.MethodOptions && origin != "" && r.Header.Get("Access-Control-Request-Method") != "" {
				if allowed {
					w.Header().Set("Access-Control-Allow-Methods", allowedMethods)
					w.Header().Set("Access-Control-Allow-Headers", allowedRequestHeaders)
					if opts.MaxAge != 0 {
						w.Header().Set("Access-Control-Max-Age", strconv.Itoa(int(opts.MaxAge.Seconds())))
					}
				}

				// Headers must be set before WriteHeader, or they are never sent.
				w.WriteHeader(http.StatusNoContent)
				return
			}

			if allowed {
				w.Header().Set("Access-Control-Expose-Headers", allowedResponseHeaders)
			}

			next.ServeHTTP(w, r)
		})
	}
}
