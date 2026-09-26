package outbound_test

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/The-Apranik/Apranik/core/outbound"
	"github.com/The-Apranik/Apranik/spec"
)

// A proxy outbound reaches its server, not the final destination, so it must
// never own a NAT. FreedomConfig takes a DatagramDialer and StreamConfig does
// not, so this is already true by construction; the test pins the behavior a
// caller sees.
func TestStreamOutboundRefusesDatagrams(t *testing.T) {
	proxy := outbound.NewStream(outbound.StreamConfig{Tag: "proxy"})

	networks := proxy.Networks()
	if len(networks) != 1 || networks[0] != spec.NetworkTCP {
		t.Errorf("Networks() = %v, want [tcp]", networks)
	}

	carrier, err := proxy.CreateDatagram(context.Background(), spec.Metadata{Network: spec.NetworkUDP})
	if !errors.Is(err, spec.ErrUnsupportedNetwork) {
		t.Errorf("CreateDatagram: %v, want spec.ErrUnsupportedNetwork", err)
	}
	if carrier != nil {
		carrier.Close()
		t.Error("CreateDatagram returned a carrier")
	}
}

// The NAT package is only for an outbound that sends UDP to final destinations
// itself. A proxy outbound that acquired one would silently NAT traffic that
// is already encapsulated, so the set of places allowed to build one is fixed
// here rather than left to review.
func TestOnlyTheFreedomOutboundBuildsANAT(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("locating the repository root: %v", err)
	}

	want := map[string]string{
		"core/outbound/freedom.go": "CreateDatagram",
	}
	got := make(map[string]string)

	fileSet := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "testdata"):
			return fs.SkipDir
		case entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go"):
			return nil
		}

		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// The nat package's own constructor is not a call site.
		if strings.HasPrefix(relative, "core/net/udp/nat/") {
			return nil
		}

		ast.Inspect(parsed, func(node ast.Node) bool {
			declaration, ok := node.(*ast.FuncDecl)
			if !ok {
				return true
			}
			ast.Inspect(declaration.Body, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := selector.X.(*ast.Ident)
				if ok && pkg.Name == "nat" && selector.Sel.Name == "New" {
					got[relative] = declaration.Name.Name
				}
				return true
			})
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}

	for file, function := range got {
		if want[file] != function {
			t.Errorf("nat.New is called from %s in %s, which is not an approved site", function, file)
		}
	}
	for file, function := range want {
		if got[file] != function {
			t.Errorf("nat.New is no longer called from %s in %s; update this test if that is deliberate", function, file)
		}
	}
}

type blockingDialer struct{}

func (blockingDialer) DialStream(ctx context.Context, _ spec.Destination) (spec.Stream, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingDialer) Close() error { return nil }

func TestADialThatNeverAnswersIsBoundedByTheDialTimeout(t *testing.T) {
	destination, err := spec.NewDestinationFromDomain("example.com", 443)
	if err != nil {
		t.Fatal(err)
	}
	outbounds := map[string]spec.Outbound{
		"freedom": outbound.NewFreedom(outbound.FreedomConfig{Tag: "freedom", Dialer: blockingDialer{}, DialTimeout: 100 * time.Millisecond}),
		"proxy":   outbound.NewStream(outbound.StreamConfig{Tag: "proxy", ServerAddr: destination, Dialer: blockingDialer{}, DialTimeout: 100 * time.Millisecond}),
	}

	for name, dialing := range outbounds {
		started := time.Now()
		_, err := dialing.DialStream(context.Background(), spec.Metadata{Network: spec.NetworkTCP, Destination: destination})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: DialStream returned %v, want a deadline error", name, err)
		}
		if waited := time.Since(started); waited > time.Second {
			t.Errorf("%s: waited %s, want about the 100ms dial timeout", name, waited)
		}
	}
}

type connectedDialer struct{ carrier spec.Stream }

func (d connectedDialer) DialStream(context.Context, spec.Destination) (spec.Stream, error) {
	return d.carrier, nil
}

func (connectedDialer) Close() error { return nil }

type silentServerClient struct{}

func (silentServerClient) RequestStream(ctx context.Context, carrier spec.Stream, _ spec.Metadata) (spec.Stream, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type pipeStream struct{ net.Conn }

func (pipeStream) CloseWrite() error { return spec.ErrNoHalfClose }

func TestAServerThatNeverAnswersTheHandshakeIsBounded(t *testing.T) {
	outboundSide, serverSide := net.Pipe()
	defer serverSide.Close()

	server, err := spec.NewDestinationFromDomain("proxy.example", 1080)
	if err != nil {
		t.Fatal(err)
	}
	proxy := outbound.NewStream(outbound.StreamConfig{
		Tag:              "proxy",
		ServerAddr:       server,
		Dialer:           connectedDialer{carrier: pipeStream{outboundSide}},
		ProxyClient:      silentServerClient{},
		HandshakeTimeout: 100 * time.Millisecond,
	})

	started := time.Now()
	_, err = proxy.DialStream(context.Background(), spec.Metadata{Network: spec.NetworkTCP, Destination: server})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("DialStream returned %v, want a deadline error", err)
	}
	if waited := time.Since(started); waited > time.Second {
		t.Errorf("waited %s, want about the 100ms handshake timeout", waited)
	}
}

func TestBlockRefusesBothNetworks(t *testing.T) {
	block := outbound.NewBlock("block")

	if networks := block.Networks(); len(networks) != 2 {
		t.Errorf("Networks() = %v, want tcp and udp", networks)
	}
	if _, err := block.DialStream(context.Background(), spec.Metadata{Network: spec.NetworkTCP}); !errors.Is(err, outbound.ErrBlocked) {
		t.Errorf("DialStream: %v, want ErrBlocked", err)
	}
	if _, err := block.CreateDatagram(context.Background(), spec.Metadata{Network: spec.NetworkUDP}); !errors.Is(err, outbound.ErrBlocked) {
		t.Errorf("CreateDatagram: %v, want ErrBlocked", err)
	}
}
