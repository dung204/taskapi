package httpapi

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	b, err := json.Marshal(v)

	if err != nil {
		return err
	}

	w.WriteHeader(status)
	w.Write(b)
	return nil
}

func writeError(w http.ResponseWriter, status int, msg string) error {
	w.Header().Add("Content-Type", "applicaion/json")
	body := map[string]string{"msg": msg}
	b, err := json.Marshal(body)

	if err != nil {
		w.WriteHeader(status)
		w.Write(b)
		return nil
	}

	return err
}
