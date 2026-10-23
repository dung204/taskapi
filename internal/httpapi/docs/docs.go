// Package docs serves the OpenAPI spec and the Swagger UI.
package docs

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed openapi.yml
var spec []byte

//go:embed ui
var uiFiles embed.FS

func Spec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(spec)
}

func UI() http.Handler {
	sub, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		panic(err) // only if the directory name above is wrong: a programming error
	}
	return http.FileServerFS(sub)
}
