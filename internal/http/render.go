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
	Code    string            `json:"code,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, &errorBody{Message: msg})
}

// writeInternalError 对外只返回稳定的通用错误码，详细原因仅落日志，
// 避免把 SQL、文件路径、加密细节等内部信息暴露给客户端。
func writeInternalError(w http.ResponseWriter, err error) {
	slog.Error("internal server error", "err", err)
	writeJSON(w, http.StatusInternalServerError, &errorBody{Message: "internal error", Code: "internal"})
}
