package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	b, err := json.Marshal(v)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		// error handling is useless here
		_, _ = w.Write([]byte("{\"error\":\"internal server error\"}"))
		fmt.Fprintln(os.Stderr, err) // TODO: replace with logger
	}

	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
