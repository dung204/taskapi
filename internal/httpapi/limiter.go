package httpapi

import (
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"time"
)

// Limiter decides whether the client identified by a key may make a request now.
// It is satisfied by [ratelimit.Limiter].
type Limiter interface {
	// Allow spends one token of the bucket of key, if there is one.
	//
	// Returns:
	//   - ok - whether the request is allowed.
	//   - retryAfter - how long to wait before retrying when the request is not
	//     allowed; zero when it is allowed. The middleware sends it as the
	//     Retry-After header of the 429 response.
	Allow(key string) (ok bool, retryAfter time.Duration)
}

type keyFunc func(r *http.Request) (key string, ok bool)

var _ keyFunc = byIP

func remoteAddr(r *http.Request) (netip.Addr, bool) {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, false
	}
	return ap.Addr().Unmap(), true
}

func throttleAddr(a netip.Addr) netip.Addr {
	if a.Is6() {
		return netip.PrefixFrom(a, 64).Masked().Addr()
	}
	return a
}

func byIP(r *http.Request) (key string, ok bool) {
	a, ok := remoteAddr(r)
	if !ok {
		return "", false
	}
	return "ip:" + throttleAddr(a).String(), true
}

func rateLimit(l Limiter, key keyFunc) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if k, ok := key(r); ok {
				if allowed, retryAfter := l.Allow(k); !allowed {
					secs := int(math.Ceil(retryAfter.Seconds()))
					w.Header().Set("Retry-After", strconv.Itoa(max(1, secs)))
					writeError(w, http.StatusTooManyRequests, "too many requests")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
