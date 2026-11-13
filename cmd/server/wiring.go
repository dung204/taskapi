package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/email"
	"github.com/dung204/taskapi/internal/httpapi"
	"github.com/dung204/taskapi/internal/metrics"
	"github.com/dung204/taskapi/internal/pubsub"
	"github.com/dung204/taskapi/internal/ratelimit"
	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/store/postgres"
	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
	"github.com/dung204/taskapi/internal/store/redis"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/worker"
)

type app struct {
	handler http.Handler
	checks  []*check
	workers []worker.Runner
}

// buildApp scaffolds stores, auth, handler & workers
func buildApp(cfg config, logger *slog.Logger, reg *prometheus.Registry, infra infra) (app, error) {
	// --- Stores ---------------------------------------------------------------

	var tasks task.Store
	switch cfg.tasks.store {
	case "memory":
		tasks = memory.NewTaskStore()
	case "postgres":
		tasks = postgres.NewTaskStore(infra.pool)
	}

	queries := sqlcgen.New(infra.pool)
	var (
		users      = postgres.NewUserStore(queries)
		userTokens = postgres.NewUserTokenStore(queries)
		sessions   = postgres.NewSessionStore(queries)
		// One store serves both the auth service (writes only) and the delivery worker (queue).
		outbox = postgres.NewOutboxStore(queries)
	)

	// --- Metrics --------------------------------------------------------------

	metrics := httpapi.Metrics{
		HTTP: metrics.NewHTTP(reg),
		Auth: metrics.NewAuth(reg),
	}

	// --- Auth building blocks -------------------------------------------------

	privKey, pubKey, err := loadEd25519Keys(cfg.jwt)
	if err != nil {
		return app{}, err
	}

	var (
		jwtService = auth.NewJWTService(auth.JWTServiceOpts{
			PrivateKey: privKey,
			PublicKey:  pubKey,
			Issuer:     cfg.jwt.issuer,
			Audiences:  cfg.jwt.audiences,
			TTL:        cfg.tokens.accessTTL,
		})

		hasher = auth.NewArgon2Hasher(auth.Argon2HasherOpts{
			Time:        cfg.argon2.time,
			MemoryKiB:   cfg.argon2.memoryKiB,
			Threads:     cfg.argon2.threads,
			Concurrency: cfg.argon2.concurrency,
		})

		// Redis-backed
		revoker      = redis.NewRevocationStore(infra.client, cfg.tokens.accessTTL)
		loginLimiter = redis.NewLoginLimiter(infra.client, redis.LoginLimiterOpts{
			FreeAttempts: cfg.login.freeAttempts,
			Backoff:      cfg.login.backoff,
			Window:       cfg.login.window,
		})
		mailRequestLimiter = redis.NewMailRequestLimiter(infra.client, redis.MailRequestLimiterOpts{
			RequestCooldown:  cfg.mail.requestCooldown,
			RequestHourlyMax: cfg.mail.requestHourlyMax,
		})
	)

	// --- Auth service ---------------------------------------------------------

	authService, err := auth.NewService(auth.ServiceDeps{
		RefreshTTL:         cfg.tokens.refreshTTL,
		RefreshGracePeriod: cfg.tokens.refreshGrace,
		// The factory must build stores from the transaction's queries, not reuse the pool-bound stores.
		Tx:         postgres.NewTransactor(infra.pool, postgres.NewAuthStores),
		Sessions:   sessions,
		UserTokens: userTokens,
		Users:      users,
		Outbox:     outbox,
		Issuer:     jwtService,
		Hasher:     hasher,
		Revoker:    auth.NewFailOpenRevoker(revoker, logger, metrics.Auth), // logout succeeds when Redis is down
		Metrics:    metrics.Auth,
		// Throttling is best effort: when Redis is down, logins and mail requests
		// go through unthrottled instead of failing.
		LoginLimiter:       auth.NewFailOpenLoginLimiter(loginLimiter, logger, metrics.Auth),
		MailRequestLimiter: auth.NewFailOpenMailRequestLimiter(mailRequestLimiter, logger, metrics.Auth),
	})
	if err != nil {
		return app{}, err
	}

	// --- HTTP -----------------------------------------------------------------

	var (
		ml      *ratelimit.MemoryLimiter // for jobs
		limiter httpapi.Limiter          // for NewHandler()
	)

	if cfg.rateLimit.rps > 0 {
		ml = ratelimit.NewMemoryLimiter(rate.Limit(cfg.rateLimit.rps), cfg.rateLimit.burst)
		limiter = ml

		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name:        "rate_limiter_tracked_clients",
			Help:        "Number of keys currently tracked by the in-memory rate limiter.",
			ConstLabels: prometheus.Labels{"limiter": "global"},
		}, func() float64 { return float64(ml.Len()) }))
	}

	handler := httpapi.NewHandler(httpapi.HandlerDeps{
		Tasks:    tasks,
		Auth:     authService,
		Verifier: jwtService,
		Revoker:  revoker,
		Logger:   logger,
		Metrics:  metrics,
		Limiter:  limiter,
		CORS:     httpapi.CORSMiddlewareOpts{AllowedOrigins: cfg.cors.origins, MaxAge: cfg.cors.maxAge},
		Origins:  httpapi.NewOriginPolicy(cfg.cors.origins),
	})

	// --- Email ----------------------------------------------------------------

	mailer, err := email.NewSMTPMailer(email.SMTPConfig{
		Host:     cfg.smtp.Host,
		Port:     cfg.smtp.Port,
		TLS:      cfg.smtp.TLS,
		Timeout:  cfg.smtp.Timeout,
		Username: cfg.smtp.Username,
		Password: cfg.smtp.Password,
		From:     cfg.smtp.From,
	})
	if err != nil {
		return app{}, err
	}

	builder, err := auth.NewEmailBuilder(auth.EmailBuilderDeps{
		BaseURL:          cfg.appBaseURL,
		VerifyEmailTTL:   cfg.tokens.verifyEmailTTL,
		ResetPasswordTTL: cfg.tokens.resetPasswordTTL,
		Users:            users,
		UserTokens:       userTokens,
	})
	if err != nil {
		return app{}, err
	}

	// --- Background workers ---------------------------------------------------

	var jobs []worker.Runner
	if ml != nil {
		jobs = append(jobs, ml)
	}

	if cfg.tasks.overdueScanEnabled {
		jobs = append(jobs, worker.NewOverdueScan(tasks, logger, cfg.tasks.overdueScanInterval))
	}

	jobs = append(jobs, worker.NewOutboxDelivery(worker.OutboxDeliveryDeps{
		PollInterval: cfg.outbox.pollInterval,
		Lease:        cfg.outbox.lease,
		BatchSize:    cfg.outbox.batchSize,
		SendTimeout:  cfg.outbox.sendTimeout,
		MaxAttempts:  cfg.outbox.maxAttempts,
		RetryBase:    cfg.outbox.retryBase,
		RetryCap:     cfg.outbox.retryCap,
		Mailer:       mailer,
		Builder:      builder,
		Outbox:       outbox,
		// email_outbox_notify trigger fires NOTIFY on insert; polling stays the fallback.
		Subscriber: pubsub.NewPostgresSubscriber(infra.pool, logger),
		Topic:      "email_outbox",
		Logger:     logger,
	}))

	jobs = append(jobs, worker.NewCleanup(
		worker.CleanupDeps{
			Interval: cfg.cleanup.interval,
			Logger:   logger,
		},
		worker.CleanupTask{Name: "user_tokens", Run: userTokens.DeleteExpired},
		worker.CleanupTask{Name: "sessions", Run: sessions.DeleteExpired},
		worker.CleanupTask{Name: "outbox_sent", Run: func(ctx context.Context, now time.Time, limit int32) (int64, error) {
			return outbox.DeleteSent(ctx, now.Add(-cfg.outbox.sentRetention), limit)
		}},
	))

	return app{
		handler: handler,
		checks:  infra.checks,
		workers: jobs,
	}, nil
}
