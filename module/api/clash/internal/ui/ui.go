package ui

import (
	"fmt"
	"net/http"
	"os"

	"github.com/we-apranik/apranik/module/api/clash/internal/web"
)

const prefix = "/ui"

type Files struct {
	root    *os.Root
	handler http.Handler
}

func Open(directory string) (*Files, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, fmt.Errorf("clash: external_ui: %w", err)
	}
	return &Files{root: root, handler: http.StripPrefix(prefix, http.FileServerFS(root.FS()))}, nil
}

func (f *Files) Close() error {
	return f.root.Close()
}

func Serves(path string) bool {
	return path == prefix || len(path) > len(prefix) && path[:len(prefix)+1] == prefix+"/"
}

func (f *Files) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		web.WriteError(w, http.StatusMethodNotAllowed, "%s is not supported here; the dashboard takes GET, HEAD", r.Method)
		return
	}
	if r.URL.Path == prefix {
		http.Redirect(w, r, prefix+"/", http.StatusMovedPermanently)
		return
	}
	f.handler.ServeHTTP(w, r)
}
