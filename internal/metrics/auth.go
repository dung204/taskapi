package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	FailOpenTimeout = "timeout"
	FailOpenError   = "error"
)

const (
	RevokeOpSession = "session"
	RevokeOpUser    = "user"
)

const (
	LimiterLogin = "login"
	LimiterMail  = "mail"
)

const (
	LoginSuccess            = "success"
	LoginInvalidCredentials = "invalid_credentials"
	LoginThrottled          = "throttled"
	LoginEmailNotVerified   = "email_not_verified"
	LoginAccountDisabled    = "account_disabled"
	LoginInvalidRequest     = "invalid_request"
	LoginError              = "error"
)

type Auth struct {
	revocationDuration prometheus.Histogram
	revokeFailed       *prometheus.CounterVec
	revocationFailOpen *prometheus.CounterVec
	limiterFailOpen    *prometheus.CounterVec
	logins             *prometheus.CounterVec
}

func NewAuth(reg *prometheus.Registry) *Auth {
	f := promauto.With(reg)

	return &Auth{
		revocationDuration: f.NewHistogram(prometheus.HistogramOpts{
			Name:    "auth_revocation_check_duration_seconds",
			Help:    "Latency of the Redis revocation check on authenticated requests.",
			Buckets: []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1},
		}),
		revokeFailed: f.NewCounterVec(prometheus.CounterOpts{
			Name: "auth_revoke_failed_total",
			Help: "Number of time that access token revocation failed, by op.",
		}, []string{"op"}),
		revocationFailOpen: f.NewCounterVec(prometheus.CounterOpts{
			Name: "auth_revocation_fail_open_total",
			Help: "Requests let through because the revocation check failed, by reason.",
		}, []string{"reason"}),
		limiterFailOpen: f.NewCounterVec(prometheus.CounterOpts{
			Name: "auth_limiter_fail_open_total",
			Help: "Redis-backed limiter calls that failed and were ignored (fail-open), by limiter.",
		}, []string{"limiter"}),
		logins: f.NewCounterVec(prometheus.CounterOpts{
			Name: "auth_logins_total",
			Help: "Login attempts by result.",
		}, []string{"result"}),
	}
}

// The methods below are safe to call on a nil *Auth and then do nothing, so
// callers can run without metrics.

func (m *Auth) ObserveRevocationCheck(d time.Duration) {
	if m == nil {
		return
	}
	m.revocationDuration.Observe(d.Seconds())
}

func (m *Auth) RevocationFailOpenInc(reason string) {
	if m == nil {
		return
	}
	m.revocationFailOpen.WithLabelValues(reason).Inc()
}

func (m *Auth) LimiterFailOpenInc(limiter string) {
	if m == nil {
		return
	}
	m.limiterFailOpen.WithLabelValues(limiter).Inc()
}

func (m *Auth) LoginsInc(result string) {
	if m == nil {
		return
	}
	m.logins.WithLabelValues(result).Inc()
}

func (m *Auth) IncRevokeFailed(op string) {
	if m == nil {
		return
	}
	m.revokeFailed.WithLabelValues(op).Inc()
}
