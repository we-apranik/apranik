package vless

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func decode(t *testing.T, text string) Settings {
	t.Helper()
	var settings Settings
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		t.Fatal(err)
	}
	return settings
}

const (
	someUUID      = "b831381d-6324-4d53-ad4f-8cda48b30811"
	anotherUUID   = "a3482e88-686a-4a58-8126-99c9df64b7bf"
	thirtyOneByte = "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
)

func TestAWrittenZeroOrNegativeDurationIsRefused(t *testing.T) {
	for _, key := range []string{"handshake_timeout"} {
		for _, written := range []string{"0s", "-1s"} {
			settings := decode(t, `{"uuid": "`+someUUID+`", "`+key+`": "`+written+`"}`)
			err := settings.Validate()
			if err == nil || !strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "greater than zero") {
				t.Errorf("%s %s: error = %v, want one naming the key and saying it must be greater than zero", key, written, err)
			}
		}
	}
}

func TestDefaultsAreTheDocumentedOnes(t *testing.T) {
	var settings Settings
	if got := settings.handshakeTimeout(); got != 10*time.Second {
		t.Errorf("handshake_timeout default = %v, want 10s", got)
	}
}

func TestValidateRefusesWhatCannotWork(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		want     string
	}{
		{"nothing", `{}`, "uuid for an outbound, or users for an inbound"},
		{"uuid that is neither a UUID nor short text", `{"uuid": "` + thirtyOneByte + `"}`, "uuid is not a UUID"},
		{"user without a name", `{"users": [{"uuid": "` + someUUID + `"}]}`, "has no name"},
		{"user without a uuid", `{"users": [{"name": "a"}]}`, `"a" has no uuid`},
		{"user with a control character in the name", `{"users": [{"name": "a\u0001", "uuid": "` + someUUID + `"}]}`, "control character"},
		{"user with a bad uuid", `{"users": [{"name": "a", "uuid": "` + thirtyOneByte + `"}]}`, "uuid that is not a UUID"},
		{"two users with one name", `{"users": [{"name": "a", "uuid": "` + someUUID + `"}, {"name": "a", "uuid": "` + anotherUUID + `"}]}`, "both named"},
		{"two users with one uuid", `{"users": [{"name": "a", "uuid": "` + someUUID + `"}, {"name": "b", "uuid": "` + strings.ToUpper(someUUID) + `"}]}`, "same uuid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := (func() error { settings := decode(t, test.settings); return settings.Validate() })()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("error = %v, want one containing %q", err, test.want)
			}
		})
	}
}

func TestShortTextIsAcceptedAsAUUIDTheWayXrayDoes(t *testing.T) {
	settings := decode(t, `{"uuid": "my-secret-id"}`)
	if err := settings.Validate(); err != nil {
		t.Errorf("a short text ID was refused: %v", err)
	}
}

func TestNoErrorHoldsAUUID(t *testing.T) {
	for _, text := range []string{
		`{"uuid": "` + thirtyOneByte + `"}`,
		`{"users": [{"name": "a", "uuid": "` + thirtyOneByte + `"}]}`,
		`{"users": [{"name": "a", "uuid": "` + someUUID + `"}, {"name": "b", "uuid": "` + someUUID + `"}]}`,
	} {
		settings := decode(t, text)
		err := settings.Validate()
		if err == nil {
			t.Fatalf("%s was accepted", text)
		}
		if strings.Contains(err.Error(), thirtyOneByte) || strings.Contains(err.Error(), someUUID) {
			t.Errorf("error %q holds a UUID", err)
		}
	}
}

func TestEachRoleRefusesTheOthersKeys(t *testing.T) {
	inboundWithUUID := decode(t, `{"uuid": "`+someUUID+`", "users": [{"name": "a", "uuid": "`+anotherUUID+`"}]}`)
	if err := inboundWithUUID.checkAsServer(); err == nil || !strings.Contains(err.Error(), "takes users, not uuid") {
		t.Errorf("inbound with a uuid: error = %v", err)
	}
	inboundWithoutUsers := decode(t, `{"uuid": "`+someUUID+`"}`)
	inboundWithoutUsers.UUID = ""
	if err := inboundWithoutUsers.checkAsServer(); err == nil {
		t.Error("an inbound without users was accepted")
	}
	outboundWithUsers := decode(t, `{"users": [{"name": "a", "uuid": "`+someUUID+`"}]}`)
	if err := outboundWithUsers.checkAsClient(); err == nil || !strings.Contains(err.Error(), "takes a uuid, not users") {
		t.Errorf("outbound with users: error = %v", err)
	}
	if err := (&Settings{}).checkAsClient(); err == nil {
		t.Error("an outbound without a uuid was accepted")
	}
}
