package httpapi

import (
	"context"
	"errors"
	"log/slog"
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

func (h *taskHandler) CreateTask(ctx context.Context, request oapigen.CreateTaskRequestObject) (oapigen.CreateTaskResponseObject, error) {
	p, err := request.Body.ToNewParams()
	if err != nil {
		return nil, err
	}

	t, err := task.New(p, time.Now())
	if errors.Is(err, task.ErrInvalidInput) {
		return oapigen.CreateTask400JSONResponse{Error: err.Error()}, nil
	}

	if err != nil {
		return nil, err
	}

	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	created, err := h.store.Create(dbCtx, *t)
	if err != nil {
		return nil, err
	}

	return oapigen.CreateTask201JSONResponse(oapigen.ToAPITask(created)), nil
}

func (h *taskHandler) ListTasks(ctx context.Context, request oapigen.ListTasksRequestObject) (oapigen.ListTasksResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	f, err := request.Params.ToListFilter()
	if errors.Is(err, task.ErrInvalidInput) {
		return oapigen.ListTasks400JSONResponse{Error: err.Error()}, nil
	}

	if err != nil {
		return nil, err
	}

	ts, err := h.store.List(dbCtx, f)
	if err != nil {
		return nil, err
	}

	return oapigen.ListTasks200JSONResponse(oapigen.ToAPITasks(ts)), nil
}

func (h *taskHandler) GetTask(ctx context.Context, request oapigen.GetTaskRequestObject) (oapigen.GetTaskResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	t, err := h.store.Get(dbCtx, request.Id)
	if errors.Is(err, task.ErrNotFound) {
		return oapigen.GetTask404JSONResponse{Error: err.Error()}, nil
	}

	if err != nil {
		return nil, err
	}

	return oapigen.GetTask200JSONResponse(oapigen.ToAPITask(t)), nil
}

func (h *taskHandler) UpdateTask(ctx context.Context, request oapigen.UpdateTaskRequestObject) (oapigen.UpdateTaskResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	p, err := request.Body.ToPatch()
	if errors.Is(err, task.ErrInvalidInput) {
		return oapigen.UpdateTask400JSONResponse{Error: err.Error()}, nil
	}

	if err != nil {
		return nil, err
	}

	t, err := h.store.Update(dbCtx, request.Id, p)
	if errors.Is(err, task.ErrNotFound) {
		return oapigen.UpdateTask404JSONResponse{Error: err.Error()}, nil
	}

	if err != nil {
		return nil, err
	}

	return oapigen.UpdateTask200JSONResponse(oapigen.ToAPITask(t)), nil
}

func (h *taskHandler) DeleteTask(ctx context.Context, request oapigen.DeleteTaskRequestObject) (oapigen.DeleteTaskResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	err := h.store.Delete(dbCtx, request.Id)
	if errors.Is(err, task.ErrNotFound) {
		return oapigen.DeleteTask404JSONResponse{Error: err.Error()}, nil
	}

	if err != nil {
		return nil, err
	}

	return oapigen.DeleteTask204Response{}, nil
}
