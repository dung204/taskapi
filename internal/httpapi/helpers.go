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
		w.Write([]byte("{\"error\":\"internal server error\"}"))
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

func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	err := json.NewDecoder(r.Body).Decode(dst)

	if err != nil {
		return writeError(w, http.StatusBadRequest, err.Error())
	}

	return nil
}
