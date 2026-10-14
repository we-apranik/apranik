package users

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/we-apranik/apranik/module/api/clash/internal/web"
	"github.com/we-apranik/apranik/spec"
)

// One entry per item of the request, in its order. A failure of one item does
// not stop the rest, and the batch is not atomic: what succeeded stays.
type batchResult struct {
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// batchCreate adds the users of an array of user objects, each as one add. The
// objects are handed on as they are: this module does not know a user's fields.
func (h handler) batchCreate(w http.ResponseWriter, r *http.Request) {
	tag := r.PathValue("tag")
	var items []json.RawMessage
	if !readBatch(w, r, &items) {
		return
	}

	results := make([]batchResult, 0, len(items))
	for index, item := range items {
		name, err := h.provider.AddInboundUser(tag, item)
		if errors.Is(err, spec.ErrAPINoSuchInbound) {
			writeUserError(w, err)
			return
		}
		results = append(results, outcome(index, name, err))
	}
	web.WriteJSON(w, http.StatusOK, map[string][]batchResult{"results": results})
}

// batchDelete removes the users named in an array of names, each as one removal.
func (h handler) batchDelete(w http.ResponseWriter, r *http.Request) {
	tag := r.PathValue("tag")
	var names []string
	if !readBatch(w, r, &names) {
		return
	}

	results := make([]batchResult, 0, len(names))
	for index, name := range names {
		err := h.provider.RemoveInboundUser(tag, name)
		if errors.Is(err, spec.ErrAPINoSuchInbound) {
			writeUserError(w, err)
			return
		}
		results = append(results, outcome(index, name, err))
	}
	web.WriteJSON(w, http.StatusOK, map[string][]batchResult{"results": results})
}

// readBatch decodes a non-empty JSON array into into. False means the answer
// was written.
func readBatch(w http.ResponseWriter, r *http.Request, into any) bool {
	body, ok := readBody(w, r, batchBodyLimit)
	if !ok {
		return false
	}
	if err := json.Unmarshal(body, into); err != nil {
		web.WriteError(w, http.StatusBadRequest, "the body must be a JSON array: %v", err)
		return false
	}
	if length := lengthOf(into); length == 0 {
		web.WriteError(w, http.StatusBadRequest, "the array is empty: there is nothing to do")
		return false
	}
	return true
}

func lengthOf(into any) int {
	switch typed := into.(type) {
	case *[]json.RawMessage:
		return len(*typed)
	case *[]string:
		return len(*typed)
	}
	return 0
}

// A name is reported when there is one: a removal always has it, and an add has
// it once it succeeded.
func outcome(index int, name string, err error) batchResult {
	if err != nil {
		return batchResult{Index: index, Name: name, Error: err.Error()}
	}
	return batchResult{Index: index, Name: name, OK: true}
}
