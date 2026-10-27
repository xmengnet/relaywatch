package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// writeJSON 输出 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

type errorBody struct {
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, &errorBody{Message: msg})
}

func writeInternalError(w http.ResponseWriter, err error) {
	slog.Error("internal server error", "err", err)
	writeError(w, http.StatusInternalServerError, err.Error())
}
