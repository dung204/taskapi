package httpapi

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/task"
)

type taskHandler struct {
	store task.Store
}

func newTaskHandler() *taskHandler {
	return &taskHandler{
		store: memory.NewTaskStore(),
	}
}

type createTaskRequest struct {
	Title       string
	Description string
	Status      task.Status
	DueAt       *time.Time
}

func (handler *taskHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	readJSON(w, r, &req)

	t, err := task.New(task.NewParams(req))

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
	}

	created, err := handler.store.Create(r.Context(), *t)

	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (handler *taskHandler) GetList(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	limit, err := strconv.Atoi(cmp.Or(query.Get("limit"), "20"))

	if err != nil {
		writeError(w, http.StatusBadRequest, "limit is invalid")
		return
	}

	if limit > 100 {
		limit = 100
	}

	offset, err := strconv.Atoi(cmp.Or(query.Get("offset"), "0"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "offset is invalid")
		return
	}

	status := query.Get("status")
	if status != "" && slices.Contains(task.AllowedStatuses, status) {
		writeError(w, http.StatusBadRequest, "status must be one of the following: "+strings.Join(task.AllowedStatuses, ", "))
		return
	}

	listFilter := task.ListFilter{
		Limit:  limit,
		Offset: offset,
		Status: task.Status(status),
	}

	tasks, err := handler.store.List(r.Context(), listFilter)

	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}

	writeJSON(w, http.StatusOK, tasks)
}
