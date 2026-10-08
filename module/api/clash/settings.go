package clash

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"time"

	"github.com/The-Apranik/Apranik/config/types"
	"github.com/The-Apranik/Apranik/shared/httpstatusranges"
)

// Settings are the module's configuration: the entry of the config's apis list
// is {"type": "clash", "settings": {...}}.
type Settings struct {
	// The IP address to listen on and the port; both are required, since an API
	// that silently listened on every interface would be a real exposure.
	Listen string `json:"listen"`
	Port   int    `json:"port"`

	// Every request must carry it as a Bearer token; required, because the
	// loopback address is reachable by every app on a phone.
	Secret types.Secret `json:"secret"`

	// The web pages (scheme://host[:port], or "*") whose scripts may call this
	// API from a browser. Absent: none may.
	AllowOrigins []string `json:"allow_origins"`

	// How many log records one /logs client may fall behind before records are
	// dropped. Absent means defaultLogBufferRecords; a written zero is refused.
	LogBufferRecords *int `json:"log_buffer_records"`

	// What a delay test uses for the values its request leaves out.
	DelayTest *DelayTestSettings `json:"delay_test"`
}

// DelayTestSettings are this module's own defaults for a delay request; core
// has none. Each is optional.
type DelayTestSettings struct {
	URL            string                 `json:"url"`
	Timeout        *types.Duration        `json:"timeout"`
	ExpectedStatus types.HTTPStatusRanges `json:"expected_status"`
}

// PROVISIONAL. The delay values are the same as a group's probe defaults, written
// again here because a module cannot import core; change both together.
const (
	defaultLogBufferRecords = 256
	maxLogBufferRecords     = 100000
	warnLogBufferRecords    = 10000

	defaultDelayTestURL            = "https://www.gstatic.com/generate_204"
	defaultDelayTestTimeout        = 10 * time.Second
	defaultDelayTestExpectedStatus = "200-299"
)

func (s *Settings) Validate() error {
	address, err := netip.ParseAddr(s.Listen)
	switch {
	case s.Listen == "":
		return errors.New("clash: listen is required, an IP address such as 127.0.0.1")
	case err != nil || address.Zone() != "":
		return fmt.Errorf("clash: listen %q is not an IP address", s.Listen)
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("clash: port %d is not between 1 and 65535", s.Port)
	}
	if s.Secret == "" {
		return errors.New("clash: secret is required")
	}
	for _, origin := range s.AllowOrigins {
		if err := checkOrigin(origin); err != nil {
			return err
		}
	}
	if err := types.CheckPositive(s.LogBufferRecords); err != nil {
		return fmt.Errorf("clash: log_buffer_records %w", err)
	}
	if s.LogBufferRecords != nil && *s.LogBufferRecords > maxLogBufferRecords {
		return fmt.Errorf("clash: log_buffer_records %d is more than %d", *s.LogBufferRecords, maxLogBufferRecords)
	}
	if s.DelayTest != nil {
		return s.DelayTest.validate()
	}
	return nil
}

func checkOrigin(origin string) error {
	if origin == "*" {
		return nil
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.User != nil {
		return fmt.Errorf("clash: allow_origins entry %q is not \"*\" or a scheme://host[:port] such as https://example.com", origin)
	}
	return nil
}

func (d *DelayTestSettings) validate() error {
	if d.URL != "" {
		parsed, err := url.Parse(d.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("clash: delay_test url %q is not an http or https URL with a host", d.URL)
		}
	}
	if err := types.CheckPositive(d.Timeout); err != nil {
		return fmt.Errorf("clash: delay_test timeout %w", err)
	}
	return nil
}

func (s *Settings) Warnings() []string {
	var warnings []string
	if address, err := netip.ParseAddr(s.Listen); err == nil && !address.IsLoopback() {
		warnings = append(warnings, fmt.Sprintf("clash: listen %s is not on this machine, so anything that can reach it and learns the secret controls this core", s.Listen))
	}
	for _, origin := range s.AllowOrigins {
		if origin == "*" {
			warnings = append(warnings, `clash: allow_origins "*" lets any web page the user opens call this API when it holds the secret`)
			break
		}
	}
	if s.LogBufferRecords != nil && *s.LogBufferRecords > warnLogBufferRecords {
		warnings = append(warnings, fmt.Sprintf("clash: log_buffer_records %d is held for each /logs client", *s.LogBufferRecords))
	}
	return warnings
}

func (s *Settings) logBufferRecords() int {
	return types.ValueOrDefault(s.LogBufferRecords, defaultLogBufferRecords)
}

// The defaults for a delay request, then the settings', then the request's own
// values over them; the three are what MeasureDelay requires.
func (s *Settings) delayTestDefaults() (rawURL string, timeout time.Duration, expectedStatus string) {
	rawURL, timeout, expectedStatus = defaultDelayTestURL, defaultDelayTestTimeout, defaultDelayTestExpectedStatus
	if s.DelayTest == nil {
		return
	}
	if s.DelayTest.URL != "" {
		rawURL = s.DelayTest.URL
	}
	if s.DelayTest.Timeout != nil {
		timeout = time.Duration(*s.DelayTest.Timeout)
	}
	if len(s.DelayTest.ExpectedStatus) > 0 {
		expectedStatus = httpstatusranges.String(s.DelayTest.ExpectedStatus.Ranges())
	}
	return
}
