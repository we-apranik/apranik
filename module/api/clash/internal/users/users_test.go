package users_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/we-apranik/apranik/module/api/clash/internal/stream"
	"github.com/we-apranik/apranik/module/api/clash/internal/testkit"
	"github.com/we-apranik/apranik/module/api/clash/internal/users"
	"github.com/we-apranik/apranik/module/api/clash/internal/web"
	"github.com/we-apranik/apranik/spec"
)

type inbound struct {
	mu    sync.Mutex
	names []string
	added []json.RawMessage
}

func (i *inbound) provider() *testkit.Provider {
	check := func(tag string) error {
		if tag != "socks-in" {
			return fmt.Errorf("%w: there is no inbound %q", spec.ErrAPINoSuchInbound, tag)
		}
		return nil
	}
	return &testkit.Provider{
		ListUsersFunc: func(tag string) ([]string, error) {
			if err := check(tag); err != nil {
				return nil, err
			}
			i.mu.Lock()
			defer i.mu.Unlock()
			return append([]string(nil), i.names...), nil
		},
		AddUserFunc: func(tag string, user json.RawMessage) (string, error) {
			if err := check(tag); err != nil {
				return "", err
			}
			var parsed struct{ Name string }
			if err := json.Unmarshal(user, &parsed); err != nil || parsed.Name == "" {
				return "", fmt.Errorf("%w: it has no name", spec.ErrInvalidUser)
			}
			i.mu.Lock()
			defer i.mu.Unlock()
			for _, existing := range i.names {
				if existing == parsed.Name {
					return "", fmt.Errorf("user %q: %w", parsed.Name, spec.ErrUserExists)
				}
			}
			i.names = append(i.names, parsed.Name)
			i.added = append(i.added, user)
			return parsed.Name, nil
		},
		RemoveUserFunc: func(tag, name string) error {
			if err := check(tag); err != nil {
				return err
			}
			i.mu.Lock()
			defer i.mu.Unlock()
			for index, existing := range i.names {
				if existing == name {
					i.names = append(i.names[:index:index], i.names[index+1:]...)
					return nil
				}
			}
			return fmt.Errorf("user %q: %w", name, spec.ErrNoSuchUser)
		},
	}
}

func mount(t *testing.T, users_ ...string) (*testkit.Env, *inbound) {
	t.Helper()
	fake := &inbound{names: users_}
	provider := fake.provider()
	return testkit.Mount(t, func(router *web.Router, _ *stream.Hub) { users.Register(router, provider) }), fake
}

func decode(t *testing.T, body string, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(body), into); err != nil {
		t.Fatalf("%q is not the JSON expected: %v", body, err)
	}
}

type results struct {
	Results []struct {
		Index int    `json:"index"`
		Name  string `json:"name"`
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	} `json:"results"`
}

func TestListAnswersTheNamesAndAnEmptyListIsNotNull(t *testing.T) {
	server, _ := mount(t, "alice", "bob")
	status, body, _ := server.Do("GET", "/inbounds/socks-in/users", "")
	if status != http.StatusOK || strings.TrimSpace(body) != `{"users":["alice","bob"]}` {
		t.Errorf("GET users = %d %q", status, body)
	}

	empty, _ := mount(t)
	if status, body, _ := empty.Do("GET", "/inbounds/socks-in/users", ""); status != http.StatusOK || strings.TrimSpace(body) != `{"users":[]}` {
		t.Errorf("GET users of an inbound with none = %d %q, want an empty array", status, body)
	}
}

func TestGetOneUserAnswersFromTheList(t *testing.T) {
	server, _ := mount(t, "alice", "bob")

	if status, body, _ := server.Do("GET", "/inbounds/socks-in/users/bob", ""); status != http.StatusOK || strings.TrimSpace(body) != `{"name":"bob"}` {
		t.Errorf("GET an existing user = %d %q", status, body)
	}
	status, body, _ := server.Do("GET", "/inbounds/socks-in/users/carol", "")
	if status != http.StatusNotFound || !strings.Contains(testkit.MessageOf(t, body), `no user "carol"`) {
		t.Errorf("GET an unknown user = %d %q", status, body)
	}
}

func TestAnUnknownInboundIsNotFoundOnEveryRoute(t *testing.T) {
	server, _ := mount(t, "alice")
	for _, request := range []struct{ method, path, body string }{
		{"GET", "/inbounds/nowhere/users", ""},
		{"GET", "/inbounds/nowhere/users/alice", ""},
		{"POST", "/inbounds/nowhere/users", `{"name": "bob"}`},
		{"DELETE", "/inbounds/nowhere/users/alice", ""},
		{"POST", "/inbounds/nowhere/users:batchCreate", `[{"name": "bob"}]`},
		{"POST", "/inbounds/nowhere/users:batchDelete", `["alice"]`},
	} {
		status, body, _ := server.Do(request.method, request.path, request.body)
		if status != http.StatusNotFound || !strings.Contains(testkit.MessageOf(t, body), `no inbound "nowhere"`) {
			t.Errorf("%s %s = %d %q, want 404 naming the inbound", request.method, request.path, status, body)
		}
	}
}

func TestAddingAUserAnswers201AndHandsTheObjectOnAsItWasSent(t *testing.T) {
	server, fake := mount(t)
	const object = `{"name": "alice", "password": "p", "extra": [1, 2]}`

	status, body, _ := server.Do("POST", "/inbounds/socks-in/users", object)

	if status != http.StatusCreated || strings.TrimSpace(body) != `{"name":"alice"}` {
		t.Errorf("POST = %d %q", status, body)
	}
	if len(fake.added) != 1 || string(fake.added[0]) != object {
		t.Errorf("the protocol was given %q, want the body untouched", fake.added)
	}
}

func TestAddingAUserMapsTheProtocolsErrors(t *testing.T) {
	server, _ := mount(t, "alice")
	for name, tc := range map[string]struct {
		body   string
		status int
		in     string
	}{
		"a taken name":    {`{"name": "alice"}`, http.StatusConflict, "already exists"},
		"an invalid user": {`{"nonsense": true}`, http.StatusBadRequest, "no name"},
		"a list":          {`[{"name": "bob"}]`, http.StatusBadRequest, "one user, a JSON object"},
		"nothing":         {``, http.StatusBadRequest, "one user, a JSON object"},
		"text":            {`password=secret-password-value`, http.StatusBadRequest, "one user, a JSON object"},
	} {
		status, body, _ := server.Do("POST", "/inbounds/socks-in/users", tc.body)
		if status != tc.status || !strings.Contains(testkit.MessageOf(t, body), tc.in) {
			t.Errorf("%s: %d %q, want %d containing %q", name, status, body, tc.status, tc.in)
		}
		if strings.Contains(body, "secret-password-value") {
			t.Errorf("%s: the answer repeats the body: %q", name, body)
		}
	}
}

func TestASingleUserBodyOver16KiBIsRefused(t *testing.T) {
	server, _ := mount(t)
	padding := strings.Repeat("a", 16<<10)

	status, body, _ := server.Do("POST", "/inbounds/socks-in/users", `{"name": "alice", "password": "`+padding+`"}`)

	if status != http.StatusRequestEntityTooLarge {
		t.Errorf("POST of a body over the limit = %d %q, want 413", status, body)
	}
}

func TestRemovingAUserAnswers204AndAnUnknownOneIs404(t *testing.T) {
	server, fake := mount(t, "alice", "bob")

	if status, _, _ := server.Do("DELETE", "/inbounds/socks-in/users/alice", ""); status != http.StatusNoContent {
		t.Errorf("DELETE = %d, want 204", status)
	}
	if strings.Join(fake.names, ",") != "bob" {
		t.Errorf("names = %v after removing alice", fake.names)
	}
	status, body, _ := server.Do("DELETE", "/inbounds/socks-in/users/alice", "")
	if status != http.StatusNotFound || !strings.Contains(testkit.MessageOf(t, body), `"alice"`) {
		t.Errorf("DELETE of an unknown user = %d %q, want 404", status, body)
	}
}

func TestAnyNameIsOnePathSegment(t *testing.T) {
	names := []string{"a/b", "سارا", "100%", "a b", "a:b", "users:batchCreate", "users:batchDelete", "..", ".", "a?b#c"}
	server, fake := mount(t, names...)

	for _, name := range names {
		path := "/inbounds/socks-in/users/" + escapedSegment(name)
		status, body, _ := server.Do("GET", path, "")
		var found struct{ Name string }
		if status != http.StatusOK {
			t.Errorf("GET %s = %d %q", path, status, body)
			continue
		}
		decode(t, body, &found)
		if found.Name != name {
			t.Errorf("GET %s answered for %q, want %q", path, found.Name, name)
		}
		if status, _, _ := server.Do("DELETE", path, ""); status != http.StatusNoContent {
			t.Errorf("DELETE %s = %d", path, status)
		}
	}
	if len(fake.names) != 0 {
		t.Errorf("names left: %v", fake.names)
	}
}

// url.PathEscape leaves "." and ".." as they are, and every HTTP client and proxy
// resolves those as directories; a name made of dots has to write them as %2E.
func escapedSegment(name string) string {
	escaped := url.PathEscape(name)
	if escaped == "." || escaped == ".." {
		return strings.ReplaceAll(escaped, ".", "%2E")
	}
	return escaped
}

func TestAMethodAPathDoesNotTakeIsAnsweredWithWhatItDoes(t *testing.T) {
	server, _ := mount(t)
	for path, allow := range map[string]string{
		"/inbounds/socks-in/users":             "GET, POST",
		"/inbounds/socks-in/users/alice":       "DELETE, GET",
		"/inbounds/socks-in/users:batchCreate": "POST",
		"/inbounds/socks-in/users:batchDelete": "POST",
	} {
		status, _, header := server.Do("PUT", path, "")
		if status != http.StatusMethodNotAllowed || header.Get("Allow") != allow {
			t.Errorf("PUT %s = %d Allow=%q, want 405 Allow=%q", path, status, header.Get("Allow"), allow)
		}
	}
}

func TestBatchCreateReportsEachUserInOrderAndKeepsGoingAfterAFailure(t *testing.T) {
	server, fake := mount(t, "alice")

	status, body, _ := server.Do("POST", "/inbounds/socks-in/users:batchCreate", `[
		{"name": "bob", "password": "1"},
		{"name": "alice", "password": "2"},
		{"nonsense": true},
		{"name": "carol", "password": "3"}
	]`)

	if status != http.StatusOK {
		t.Fatalf("status = %d %q", status, body)
	}
	var got results
	decode(t, body, &got)
	want := []struct {
		name string
		ok   bool
		in   string
	}{{"bob", true, ""}, {"", false, "already exists"}, {"", false, "no name"}, {"carol", true, ""}}
	if len(got.Results) != len(want) {
		t.Fatalf("%d results, want %d: %q", len(got.Results), len(want), body)
	}
	for index, w := range want {
		r := got.Results[index]
		if r.Index != index || r.Name != w.name || r.OK != w.ok || !strings.Contains(r.Error, w.in) {
			t.Errorf("result %d = %+v, want name %q ok %v error containing %q", index, r, w.name, w.ok, w.in)
		}
	}
	if strings.Join(fake.names, ",") != "alice,bob,carol" {
		t.Errorf("names = %v: what succeeded must stay", fake.names)
	}
}

func TestBatchDeleteReportsEachNameInOrderAndKeepsGoingAfterAFailure(t *testing.T) {
	server, fake := mount(t, "alice", "bob", "carol")

	status, body, _ := server.Do("POST", "/inbounds/socks-in/users:batchDelete", `["bob", "nobody", "alice"]`)

	if status != http.StatusOK {
		t.Fatalf("status = %d %q", status, body)
	}
	var got results
	decode(t, body, &got)
	if len(got.Results) != 3 || !got.Results[0].OK || got.Results[1].OK || !got.Results[2].OK {
		t.Fatalf("results = %+v", got.Results)
	}
	if got.Results[1].Name != "nobody" || !strings.Contains(got.Results[1].Error, "no user") {
		t.Errorf("the failed removal = %+v, want its name and why", got.Results[1])
	}
	if strings.Join(fake.names, ",") != "carol" {
		t.Errorf("names = %v", fake.names)
	}
}

func TestABatchOfThousandsOfUsersIsOneRequest(t *testing.T) {
	server, fake := mount(t)
	var request strings.Builder
	request.WriteString("[")
	for index := range 5000 {
		if index > 0 {
			request.WriteString(",")
		}
		fmt.Fprintf(&request, `{"name": "user-%d", "password": "password-%d"}`, index, index)
	}
	request.WriteString("]")

	status, body, _ := server.Do("POST", "/inbounds/socks-in/users:batchCreate", request.String())

	if status != http.StatusOK || len(fake.names) != 5000 {
		t.Errorf("status %d, %d users added", status, len(fake.names))
	}
	var got results
	decode(t, body, &got)
	for _, r := range got.Results {
		if !r.OK {
			t.Fatalf("a user failed: %+v", r)
		}
	}
}

func TestABatchThatIsNotAnArrayOrIsEmptyIsRefused(t *testing.T) {
	server, _ := mount(t, "alice")
	for _, path := range []string{"/inbounds/socks-in/users:batchCreate", "/inbounds/socks-in/users:batchDelete"} {
		for name, body := range map[string]string{
			"an object": `{"name": "bob"}`,
			"empty":     `[]`,
			"null":      `null`,
			"nothing":   ``,
			"text":      `password=secret-password-value`,
		} {
			status, answer, _ := server.Do("POST", path, body)
			if status != http.StatusBadRequest {
				t.Errorf("POST %s with %s = %d %q, want 400", path, name, status, answer)
			}
			if strings.Contains(answer, "secret-password-value") {
				t.Errorf("POST %s with %s: the answer repeats the body: %q", path, name, answer)
			}
		}
	}
	if status, _, _ := server.Do("POST", "/inbounds/socks-in/users:batchDelete", `[1, 2]`); status != http.StatusBadRequest {
		t.Errorf("a batch of numbers = %d, want 400", status)
	}
}

func TestABatchOver8MiBIsRefused(t *testing.T) {
	server, _ := mount(t)
	body := `["` + strings.Repeat("a", 8<<20) + `"]`

	if status, _, _ := server.Do("POST", "/inbounds/socks-in/users:batchDelete", body); status != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", status)
	}
}

func TestAProviderErrorItDoesNotKnowIsAServerError(t *testing.T) {
	provider := &testkit.Provider{ListUsersFunc: func(string) ([]string, error) { return nil, errors.New("boom") }}
	server := testkit.Mount(t, func(router *web.Router, _ *stream.Hub) { users.Register(router, provider) })

	if status, _, _ := server.Do("GET", "/inbounds/socks-in/users", ""); status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", status)
	}
}
