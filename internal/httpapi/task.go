package httpapi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
	"uuid"

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

type createTaskRequest struct {
	Title       string      `json:"title"`
	Description *string     `json:"description"`
	Status      task.Status `json:"status"`
	DueAt       *string     `json:"due_at"`
}

func (r createTaskRequest) toNewParams() (task.NewParams, error) {
	dueAt, err := parseTime("due_at", r.DueAt)
	if err != nil {
		return task.NewParams{}, err
	}

	return task.NewParams{
		Title:       r.Title,
		Description: r.Description,
		Status:      r.Status,
		DueAt:       dueAt,
	}, nil
}

type updateTaskRequest struct {
	Title       *string      `json:"title"`
	Description *string      `json:"description"`
	Status      *task.Status `json:"status"`
	DueAt       *string      `json:"due_at"`
}

func (r updateTaskRequest) toPatch() (task.Patch, error) {
	dueAt, err := parseTime("due_at", r.DueAt)
	if err != nil {
		return task.Patch{}, err
	}

	return task.Patch{
		Title:       r.Title,
		Description: r.Description,
		Status:      r.Status,
		DueAt:       dueAt,
	}, nil
}

const dbTimeout = 3 * time.Second

func (h *taskHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest

	err := readJSON(w, r, &req)
	if err != nil {
		return
	}

	p, err := req.toNewParams()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	t, err := task.New(p)

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

	writeJSON(w, http.StatusCreated, created)
}

func (h *taskHandler) GetList(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	limit, err := strconv.Atoi(cmp.Or(query.Get("limit"), "20"))

	if err != nil || limit < 1 {
		writeError(w, http.StatusBadRequest, "limit is invalid")
		return
	}

	if limit > 100 {
		limit = 100
	}

	offset, err := strconv.Atoi(cmp.Or(query.Get("offset"), "0"))
	if err != nil || offset < 0 {
		writeError(w, http.StatusBadRequest, "offset is invalid")
		return
	}

	status := query.Get("status")
	if status != "" && !task.Status(status).IsValidForGet() {
		writeError(w, http.StatusBadRequest, task.ErrInvalidGetStatus.Error())
		return
	}

	listFilter := task.ListFilter{
		Limit:  limit,
		Offset: offset,
		Status: task.Status(status),
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	tasks, err := h.store.List(ctx, listFilter)

	if hasError := handleStoreError(ctx, w, h.logger, err); hasError {
		return
	}

	writeJSON(w, http.StatusOK, tasks)
}

func (h *taskHandler) GetOne(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	t, err := h.store.Get(ctx, id)

	if hasError := handleStoreError(ctx, w, h.logger, err); hasError {
		return
	}

	writeJSON(w, http.StatusOK, t)
}

func (h *taskHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req updateTaskRequest
	err = readJSON(w, r, &req)

	if err != nil {
		fmt.Fprintln(os.Stderr, err) // TODO: replace with logger
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	p, err := req.toPatch()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	t, err := h.store.Update(ctx, id, p)

	if hasError := handleStoreError(ctx, w, h.logger, err); hasError {
		return
	}

	writeJSON(w, http.StatusOK, t)
}

func (h *taskHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	err = h.store.Delete(ctx, id)

	if hasError := handleStoreError(ctx, w, h.logger, err); hasError {
		return
	}

	w.WriteHeader(http.StatusNoContent)
	w.Write(nil)
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
		l.ErrorContext(ctx, err.Error())
		return true
	}

	if errors.Is(err, context.Canceled) {
		return true
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		l.ErrorContext(ctx, err.Error())
		return true
	}

	return false
}
