package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/dung204/taskapi/internal/httpapi/oapigen"
	"github.com/dung204/taskapi/internal/task"
)

type taskHandler struct {
	store  task.Store
	logger *slog.Logger
}

func newTaskHandler(s task.Store, l *slog.Logger) *taskHandler {
	return &taskHandler{
		store:  s,
		logger: l,
	}
}

const dbTimeout = 3 * time.Second

func (h *taskHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var req oapigen.CreateTaskRequest

	err := readJSON(w, r, &req)
	if err != nil {
		return
	}

	p, err := req.ToNewParams()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	t, err := task.New(p, time.Now())

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	created, err := h.store.Create(ctx, *t)

	if hasError := handleStoreError(ctx, w, h.logger, err); hasError {
		return
	}

	writeJSON(w, http.StatusCreated, oapigen.ToAPITask(created))
}

func (h *taskHandler) ListTasks(w http.ResponseWriter, r *http.Request, params oapigen.ListTasksParams) {

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	f, err := params.ToListFilter()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
	}

	tasks, err := h.store.List(ctx, f)

	if hasError := handleStoreError(ctx, w, h.logger, err); hasError {
		return
	}

	writeJSON(w, http.StatusOK, oapigen.ToAPITasks(tasks))
}

func (h *taskHandler) GetTask(w http.ResponseWriter, r *http.Request, id oapigen.TaskId) {
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	t, err := h.store.Get(ctx, id)

	if hasError := handleStoreError(ctx, w, h.logger, err); hasError {
		return
	}

	writeJSON(w, http.StatusOK, oapigen.ToAPITask(t))
}

func (h *taskHandler) UpdateTask(w http.ResponseWriter, r *http.Request, id oapigen.TaskId) {
	var req oapigen.UpdateTaskRequest
	err := readJSON(w, r, &req)

	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	p, err := req.ToPatch()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	t, err := h.store.Update(ctx, id, p)

	if hasError := handleStoreError(ctx, w, h.logger, err); hasError {
		return
	}

	writeJSON(w, http.StatusOK, oapigen.ToAPITask(t))
}

func (h *taskHandler) DeleteTask(w http.ResponseWriter, r *http.Request, id oapigen.TaskId) {
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	err := h.store.Delete(ctx, id)

	if hasError := handleStoreError(ctx, w, h.logger, err); hasError {
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func handleStoreError(ctx context.Context, w http.ResponseWriter, l *slog.Logger, err error) bool {
	if errors.Is(err, task.ErrInvalidInput) {
		writeError(w, http.StatusBadRequest, err.Error())
		return true
	}

	if errors.Is(err, task.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return true
	}

	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusServiceUnavailable, "service unavailable")
		return true
	}

	if errors.Is(err, task.ErrServiceUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "service unavailable")
		l.ErrorContext(ctx, "store error", "error", err)
		return true
	}

	if errors.Is(err, context.Canceled) {
		return true
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		l.ErrorContext(ctx, "store error", "error", err)
		return true
	}

	return false
}
