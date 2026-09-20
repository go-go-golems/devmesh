package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/wesen/devmesh/internal/daemon"
)

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

var statusForCode = map[string]int{
	daemon.CodeInvalidRequest:       http.StatusBadRequest,
	daemon.CodeInvalidName:          http.StatusBadRequest,
	daemon.CodeInvalidBackend:       http.StatusBadRequest,
	daemon.CodeUnsupportedKind:      http.StatusBadRequest,
	daemon.CodeUnauthorized:         http.StatusUnauthorized,
	daemon.CodeRegistrationNotFound: http.StatusNotFound,
	daemon.CodeServiceNotFound:      http.StatusNotFound,
	daemon.CodeNameConflict:         http.StatusConflict,
	daemon.CodePortExhausted:        http.StatusServiceUnavailable,
	daemon.CodeDockerUnavailable:    http.StatusServiceUnavailable,
	daemon.CodeInternal:             http.StatusInternalServerError,
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var de *daemon.Error
	if errors.As(err, &de) {
		status := statusForCode[de.Code]
		if status == 0 {
			status = http.StatusInternalServerError
		}
		if status >= 500 {
			logger.Error("api_error", "code", de.Code, "error", de.Message)
		}
		writeJSON(w, status, errorEnvelope{Error: errorBody{Code: de.Code, Message: de.Message}})
		return
	}
	logger.Error("api_error", "code", daemon.CodeInternal, "error", err)
	writeJSON(w, http.StatusInternalServerError, errorEnvelope{Error: errorBody{
		Code:    daemon.CodeInternal,
		Message: "internal error",
	}})
}
