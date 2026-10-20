package config

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/registry"
)

const maxDocumentSize = 4 << 20

const DefaultFetchTimeout = 30 * time.Second

func LoadFile(path string, modules *registry.Registry) (*blueprint.Blueprint, Diagnostics) {
	data, err := os.ReadFile(path)
	if err != nil {
		var diagnostics Diagnostics
		diagnostics.errorf("", "reading %s: %v", path, err)
		return nil, diagnostics
	}
	return load(data, modules, nil, filepath.Dir(path))
}

func LoadURL(ctx context.Context, url string, modules *registry.Registry) (*blueprint.Blueprint, Diagnostics) {
	var diagnostics Diagnostics
	if strings.HasPrefix(url, "http://") {
		diagnostics.warnf("", "%s is fetched over plain HTTP, so anyone on the path can alter it; use https", url)
	}

	data, err := fetch(ctx, url)
	if err != nil {
		diagnostics.report("", err)
		return nil, diagnostics
	}
	return load(data, modules, diagnostics, executableDirectory())
}

func executableDirectory() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	return filepath.Dir(executable)
}

// Parse and both checks together, so no loader can forget one.
func load(data []byte, modules *registry.Registry, diagnostics Diagnostics, baseDirectory string) (*blueprint.Blueprint, Diagnostics) {
	coreBlueprint, parsed := Parse(data, modules, WithBaseDirectory(baseDirectory))
	diagnostics = append(diagnostics, parsed...)
	if coreBlueprint != nil {
		diagnostics = append(diagnostics, Check(coreBlueprint)...)
		diagnostics = append(diagnostics, CheckDatasets(coreBlueprint)...)
	}
	return coreBlueprint, diagnostics
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultFetchTimeout)
		defer cancel()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	request.Header.Set("Accept", "application/json")

	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", url, response.Status)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxDocumentSize+1))
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	if len(data) > maxDocumentSize {
		return nil, fmt.Errorf("fetching %s: config file is larger than %d bytes", url, maxDocumentSize)
	}
	return data, nil
}
