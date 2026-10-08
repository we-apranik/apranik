package proxies

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/The-Apranik/Apranik/module/api/clash/internal/web"
	"github.com/The-Apranik/Apranik/spec"
)

// How many members of a group are measured at once.
const groupDelayConcurrency = 8

type delayRequest struct {
	url            string
	timeout        time.Duration
	expectedStatus string
}

// requestOf reads url, timeout (milliseconds) and expected from the query, and
// fills what it leaves out from the defaults. Mihomo names the last one
// "expected"; "expected_status" is the name Apranik's own settings use.
func (h handler) requestOf(r *http.Request) (delayRequest, error) {
	query := r.URL.Query()
	request := delayRequest{url: h.defaults.URL, timeout: h.defaults.Timeout, expectedStatus: h.defaults.ExpectedStatus}
	if given := query.Get("url"); given != "" {
		request.url = given
	}
	if given := query.Get("timeout"); given != "" {
		milliseconds, err := strconv.Atoi(given)
		if err != nil || milliseconds <= 0 {
			return delayRequest{}, web.BadParameter("timeout is a whole number of milliseconds greater than zero, not %q", given)
		}
		request.timeout = time.Duration(milliseconds) * time.Millisecond
	}
	for _, name := range []string{"expected", "expected_status"} {
		if given := query.Get(name); given != "" {
			request.expectedStatus = given
		}
	}
	return request, nil
}

type delayBody struct {
	Delay int64 `json:"delay"`
}

func (h handler) delay(w http.ResponseWriter, r *http.Request) {
	request, err := h.requestOf(r)
	if err != nil {
		web.WriteError(w, http.StatusBadRequest, "%v", err)
		return
	}
	delay, err := h.measure(r.Context(), r.PathValue("name"), request)
	if err != nil {
		writeDelayError(w, err)
		return
	}
	web.WriteJSON(w, http.StatusOK, delayBody{Delay: milliseconds(delay)})
}

// Dashboards read a delay of zero as a failure, so a very fast answer is 1.
func milliseconds(delay time.Duration) int64 { return max(delay.Milliseconds(), 1) }

func (h handler) measure(ctx context.Context, outboundTag string, request delayRequest) (time.Duration, error) {
	return h.provider.MeasureDelay(ctx, outboundTag, request.url, request.timeout, request.expectedStatus)
}

func writeDelayError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, spec.ErrAPINoSuchOutbound):
		web.WriteError(w, http.StatusNotFound, "%v", err)
	case errors.Is(err, spec.ErrAPIInvalidDelayTest):
		web.WriteError(w, http.StatusBadRequest, "%v", err)
	case errors.Is(err, context.DeadlineExceeded):
		web.WriteError(w, http.StatusGatewayTimeout, "the delay test timed out: %v", err)
	default:
		web.WriteError(w, http.StatusServiceUnavailable, "the delay test failed: %v", err)
	}
}

// groupDelay measures each member of a group and answers {member: milliseconds}.
// A member that failed is left out, so a dashboard shows it as untested.
func (h handler) groupDelay(w http.ResponseWriter, r *http.Request) {
	groupTag := r.PathValue("name")
	state, err := h.provider.OutboundState(groupTag)
	if err != nil {
		web.WriteProviderError(w, err)
		return
	}
	if state.Group == nil {
		web.WriteError(w, http.StatusNotFound, "%s is not a group", groupTag)
		return
	}
	request, err := h.requestOf(r)
	if err != nil {
		web.WriteError(w, http.StatusBadRequest, "%v", err)
		return
	}

	var (
		mu      sync.Mutex
		results = make(map[string]int64, len(state.Group.MemberTags))
		invalid error
		wait    sync.WaitGroup
		slots   = make(chan struct{}, groupDelayConcurrency)
	)
	for _, memberTag := range state.Group.MemberTags {
		wait.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			delay, err := h.measure(r.Context(), memberTag, request)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				results[memberTag] = milliseconds(delay)
			case errors.Is(err, spec.ErrAPIInvalidDelayTest):
				invalid = err
			}
		})
	}
	wait.Wait()
	if invalid != nil {
		writeDelayError(w, invalid)
		return
	}
	web.WriteJSON(w, http.StatusOK, results)
}
