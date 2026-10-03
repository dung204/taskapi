package httpapi

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/task"
)

type taskHandler struct {
	store task.Store
}

func newTaskHandler(store task.Store) *taskHandler {
	return &taskHandler{
		store,
	}
}

type createTaskRequest struct {
	Title       string      `json:"title"`
	Description *string     `json:"description"`
	Status      task.Status `json:"status"`
	DueAt       *time.Time  `json:"due_at"`
}

type updateTaskRequest struct {
	Title       *string      `json:"title"`
	Description *string      `json:"description"`
	Status      *task.Status `json:"status"`
	DueAt       *time.Time   `json:"due_at"`
}

func (handler *taskHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest

	err := readJSON(w, r, &req)
	if err != nil {
		fmt.Println(err) // TODO: replace with logger
		return
	}

	t, err := task.New(task.NewParams(req))

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	created, err := handler.store.Create(r.Context(), *t)

	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}

	if errors.Is(err, context.Canceled) {
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		fmt.Println(err) // TODO: replace with logger
		return
	}

	writeJSON(w, http.StatusCreated, created)
}

func (handler *taskHandler) GetList(w http.ResponseWriter, r *http.Request) {
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

	listFilter := task.ListFilter{
		Limit:  limit,
		Offset: offset,
		Status: task.Status(status),
	}

	tasks, err := handler.store.List(r.Context(), listFilter)

	if errors.Is(err, task.ErrInvalidInput) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}

	if errors.Is(err, context.Canceled) {
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		fmt.Println(err) // TODO: replace with logger
		return
	}

	writeJSON(w, http.StatusOK, tasks)
}

func (handler *taskHandler) GetOne(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	t, err := handler.store.Get(r.Context(), id)

	if errors.Is(err, task.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}

	if errors.Is(err, context.Canceled) {
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		fmt.Println(err) // TODO: replace with logger
		return
	}

	writeJSON(w, http.StatusOK, t)
}

func (handler *taskHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req updateTaskRequest
	err = readJSON(w, r, &req)

	if err != nil {
		fmt.Println(err) // TODO: replace with logger
		return
	}

	t, err := handler.store.Update(r.Context(), id, task.Patch(req))

	if errors.Is(err, task.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}

	if errors.Is(err, context.Canceled) {
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		fmt.Println(err) // TODO: replace with logger
		return
	}

	writeJSON(w, http.StatusOK, t)
}

func (handler *taskHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))

	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	err = handler.store.Delete(r.Context(), id)

	if errors.Is(err, task.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	if errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusServiceUnavailable, "service unavailable")
		return
	}

	if errors.Is(err, context.Canceled) {
		return
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		fmt.Println(err) // TODO: replace with logger
		return
	}

	w.WriteHeader(http.StatusNoContent)
	w.Write(nil)
}
