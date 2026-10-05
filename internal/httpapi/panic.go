package httpapi

import "net/http"

type panicHandler struct{}

func newPanicHandler() panicHandler {
	return panicHandler{}
}

func (panicHandler) TestPanic(_ http.ResponseWriter, _ *http.Request) {
	panic("testing panic...")
}
