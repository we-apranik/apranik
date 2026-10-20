package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/we-apranik/apranik/spec"
)

func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func WriteError(w http.ResponseWriter, status int, format string, arguments ...any) {
	WriteJSON(w, status, map[string]string{"message": fmt.Sprintf(format, arguments...)})
}

func WriteNoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

func WriteProviderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, spec.ErrAPIStatisticsDisabled):
		WriteError(w, http.StatusServiceUnavailable, "statistics are disabled; set stats.enabled to true to count traffic")
	case errors.Is(err, spec.ErrAPILiveConversationsDisabled):
		WriteError(w, http.StatusServiceUnavailable, "live conversations are not tracked, so they cannot be listed or closed; set stats.tracking.enabled to true")
	case errors.Is(err, spec.ErrAPINoSuchOutbound), errors.Is(err, spec.ErrAPINoSuchLiveConversation):
		WriteError(w, http.StatusNotFound, "%v", err)
	case errors.Is(err, spec.ErrAPINoLogFileOutput):
		WriteError(w, http.StatusNotFound, "no file log output is configured")
	default:
		WriteError(w, http.StatusInternalServerError, "%v", err)
	}
}

type BadParameterError struct{ message string }

func (e BadParameterError) Error() string { return e.message }

func BadParameter(format string, arguments ...any) error {
	return BadParameterError{message: fmt.Sprintf(format, arguments...)}
}
