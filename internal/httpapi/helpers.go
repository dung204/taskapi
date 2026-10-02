package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	b, err := json.Marshal(v)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Print(err) // TODO: replace with logger
		return err
	}

	w.WriteHeader(status)
	w.Write(b)
	return nil
}

func writeError(w http.ResponseWriter, status int, msg string) error {
	return writeJSON(w, status, map[string]string{"error": msg})
}
