package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	TaskPermissionEditContent  = "edit_content"
	TaskPermissionChangeStatus = "change_status"
	TaskPermissionAssign       = "assign"
	TaskPermissionDelete       = "delete"
	TaskPermissionManageShares = "manage_shares"
)

const (
	InvitationEventCreated   = "created"
	InvitationEventAdded     = "added"
	InvitationEventAccepted  = "accepted"
	InvitationEventDeclined  = "declined"
	InvitationEventCancelled = "cancelled"
	InvitationEventExpired   = "expired"
)

type Task struct {
	authzDenied *prometheus.CounterVec
	invitations *prometheus.CounterVec
	// limiterFailOpen counts limiter calls that failed and were ignored.
	limiterFailOpen *prometheus.CounterVec
}

func NewTask(reg *prometheus.Registry) *Task {
	f := promauto.With(reg)

	return &Task{
		authzDenied: f.NewCounterVec(prometheus.CounterOpts{
			Name: "task_authz_denied_total",
			Help: "Number of task actions denied by authorization, by the permission that was missing.",
		}, []string{"permission"}),
		invitations: f.NewCounterVec(prometheus.CounterOpts{
			Name: "task_invitations_total",
			Help: "Number of task invitation events, by event (created, added, accepted, declined, cancelled, expired).",
		}, []string{"event"}),
		limiterFailOpen: f.NewCounterVec(prometheus.CounterOpts{
			Name: "task_limiter_fail_open_total",
			Help: "Redis-backed invitation limiter calls that failed and were ignored (fail-open), by limiter.",
		}, []string{"limiter"}),
	}
}

func (m *Task) AuthzDeniedInc(permission string) {
	if m == nil {
		return
	}
	m.authzDenied.WithLabelValues(permission).Inc()
}

func (m *Task) InvitationsInc(event string) {
	if m == nil {
		return
	}
	m.invitations.WithLabelValues(event).Inc()
}

func (m *Task) ExpiredInvitationsAdd(count int64) {
	if m == nil {
		return
	}
	m.invitations.WithLabelValues(InvitationEventExpired).Add(float64(count))
}

func (m *Task) LimiterFailOpenInc(limiter string) {
	if m == nil {
		return
	}
	m.limiterFailOpen.WithLabelValues(limiter).Inc()
}
