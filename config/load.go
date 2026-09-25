package config

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/registry"
)

// A config file is small. The cap stops a wrong URL from streaming a video into
// memory; it is not a schema limit.
const maxDocumentSize = 4 << 20

// Used when the caller's context carries no deadline of its own.
const DefaultFetchTimeout = 30 * time.Second

// LoadFile reads, parses and checks a config file on disk.
func LoadFile(path string, modules *registry.Registry) (*blueprint.Blueprint, Diagnostics) {
	data, err := os.ReadFile(path)
	if err != nil {
		var diagnostics Diagnostics
		diagnostics.errorf("", "reading %s: %v", path, err)
		return nil, diagnostics
	}
	return load(data, modules, nil)
}

// LoadURL fetches, parses and checks a config file over http or https. A plain
// http fetch succeeds with a warning: anyone on the path could have altered a
// config file that crossed the wire in the clear.
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
	return load(data, modules, diagnostics)
}

// Parse and Check together, so no loader can forget the second half.
func load(data []byte, modules *registry.Registry, diagnostics Diagnostics) (*blueprint.Blueprint, Diagnostics) {
	coreBlueprint, parsed := Parse(data, modules)
	diagnostics = append(diagnostics, parsed...)
	if coreBlueprint != nil {
		diagnostics = append(diagnostics, Check(coreBlueprint)...)
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
