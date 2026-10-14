package spec_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/we-apranik/apranik/spec"
)

type fakeAPIModule struct{}

func (fakeAPIModule) Validate() error                       { return nil }
func (fakeAPIModule) Warnings() []string                    { return nil }
func (fakeAPIModule) Info() spec.ModuleInfo                 { return spec.ModuleInfo{Name: "fake"} }
func (fakeAPIModule) NewAPIServer() (spec.APIServer, error) { return nil, nil }

var _ spec.APIModule = fakeAPIModule{}

func TestEveryAPIErrorIsDistinctAndSaysItIsFromSpec(t *testing.T) {
	apiErrors := []error{
		spec.ErrAPIStatisticsDisabled, spec.ErrAPILiveConversationsDisabled, spec.ErrAPINoSuchOutbound,
		spec.ErrAPIOutboundNotSelectable, spec.ErrAPINoSuchMember, spec.ErrAPINoSuchLiveConversation,
		spec.ErrAPINoLogFileOutput, spec.ErrAPIInvalidDelayTest, spec.ErrAPIInvalidDNSQuery,
		spec.ErrAPIInvalidConfiguration, spec.ErrAPIReloadInProgress, spec.ErrAPIReloadUnavailable,
	}
	for index, err := range apiErrors {
		if !strings.HasPrefix(err.Error(), "spec: ") {
			t.Errorf("%q does not start with spec:", err)
		}
		for _, other := range apiErrors[index+1:] {
			if errors.Is(err, other) {
				t.Errorf("%q and %q are the same error", err, other)
			}
		}
	}
}
