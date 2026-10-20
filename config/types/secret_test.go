package types

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const password = "correct-horse-battery-staple"

func TestSecretDoesNotLeakThroughFmt(t *testing.T) {
	secret := Secret(password)

	formatted := map[string]string{
		"%v":       fmt.Sprintf("%v", secret),
		"%s":       fmt.Sprintf("%s", secret),
		"%q":       fmt.Sprintf("%q", secret),
		"%+v":      fmt.Sprintf("%+v", secret),
		"%#v":      fmt.Sprintf("%#v", secret),
		"String()": secret.String(),
		"Errorf":   fmt.Errorf("auth failed for %v", secret).Error(),
		"print":    fmt.Sprint(secret),
	}

	for verb, output := range formatted {
		if strings.Contains(output, password) {
			t.Errorf("%s leaked the secret: %s", verb, output)
		}
		if !strings.Contains(output, redacted) {
			t.Errorf("%s = %s, want it to contain %s", verb, output, redacted)
		}
	}

	if quoted := fmt.Sprintf("%q", secret); quoted != `"`+redacted+`"` {
		t.Errorf("%%q = %s, want %q", quoted, `"`+redacted+`"`)
	}
}

func TestSecretDoesNotLeakAsStructField(t *testing.T) {
	settings := struct {
		User     string `json:"user"`
		Password Secret `json:"password"`
	}{User: "alice", Password: Secret(password)}

	for _, output := range []string{
		fmt.Sprintf("%v", settings),
		fmt.Sprintf("%+v", settings),
		fmt.Sprintf("%#v", settings),
	} {
		if strings.Contains(output, password) {
			t.Errorf("leaked: %s", output)
		}
	}

	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), password) {
		t.Errorf("json.Marshal leaked the secret: %s", encoded)
	}
	if !strings.Contains(string(encoded), redacted) {
		t.Errorf("json.Marshal = %s, want it to contain %s", encoded, redacted)
	}
}

func TestSecretDecodesAndReveals(t *testing.T) {
	var decoded struct {
		Password Secret `json:"password"`
	}
	if err := json.Unmarshal([]byte(`{"password":"`+password+`"}`), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Password.Reveal() != password {
		t.Errorf("Reveal() = %q, want %q", decoded.Password.Reveal(), password)
	}
}
