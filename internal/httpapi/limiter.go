package httpapi

import (
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"time"
)

type Limiter interface {
	Allow(key string) (ok bool, retryAfter time.Duration)
}

type keyFunc func(r *http.Request) (key string, ok bool)

var _ keyFunc = byIP

func byIP(r *http.Request) (key string, ok bool) {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return "", false
	}

	addr := ap.Addr().Unmap()
	if addr.Is6() {
		addr = netip.PrefixFrom(addr, 64).Masked().Addr()
	}

	return "ip:" + addr.String(), true
}

func rateLimit(l Limiter, key keyFunc) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if k, ok := key(r); ok {
				if ok, retryAfter := l.Allow(k); !ok {
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
