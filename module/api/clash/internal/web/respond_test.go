package web_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
)

func TestWriteErrorAnswersAMessageWithTheStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	web.WriteError(recorder, http.StatusTeapot, "no %s here", "tea")
	if recorder.Code != http.StatusTeapot || strings.TrimSpace(recorder.Body.String()) != `{"message":"no tea here"}` || recorder.Header().Get("Content-Type") != "application/json" {
		t.Errorf("%d %q %v", recorder.Code, recorder.Body.String(), recorder.Header())
	}
}

func TestProviderErrorsHaveTheStatusDashboardsExpect(t *testing.T) {
	for name, test := range map[string]struct {
		err    error
		status int
		in     string
	}{
		"statistics off":       {spec.ErrAPIStatisticsDisabled, http.StatusServiceUnavailable, "stats.enabled"},
		"tracking off":         {spec.ErrAPILiveConversationsDisabled, http.StatusServiceUnavailable, "stats.tracking.enabled"},
		"no such outbound":     {spec.ErrAPINoSuchOutbound, http.StatusNotFound, "no outbound"},
		"no such conversation": {spec.ErrAPINoSuchLiveConversation, http.StatusNotFound, "no live conversation"},
		"no log file":          {spec.ErrAPINoLogFileOutput, http.StatusNotFound, "no file log output"},
		"wrapped":              {errors.Join(errors.New("detail"), spec.ErrAPINoSuchOutbound), http.StatusNotFound, "no outbound"},
		"anything else":        {errors.New("boom"), http.StatusInternalServerError, "boom"},
	} {
		recorder := httptest.NewRecorder()
		web.WriteProviderError(recorder, test.err)
		if recorder.Code != test.status || !strings.Contains(recorder.Body.String(), test.in) {
			t.Errorf("%s: %d %q, want %d containing %q", name, recorder.Code, recorder.Body.String(), test.status, test.in)
		}
	}
}

func TestWriteNoContentHasNoBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	web.WriteNoContent(recorder)
	if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 {
		t.Errorf("%d %q", recorder.Code, recorder.Body.String())
	}
}

func TestABadParameterCarriesItsMessage(t *testing.T) {
	err := web.BadParameter("timeout is %s", "bad")
	var bad web.BadParameterError
	if !errors.As(err, &bad) || err.Error() != "timeout is bad" {
		t.Errorf("err = %v", err)
	}
}
