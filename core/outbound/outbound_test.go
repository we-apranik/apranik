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

	datagram, err := proxy.CreateDatagram(context.Background(), spec.Metadata{Network: spec.NetworkUDP})
	if !errors.Is(err, spec.ErrUnsupportedNetwork) {
		t.Errorf("CreateDatagram: %v, want spec.ErrUnsupportedNetwork", err)
	}
	if datagram != nil {
		datagram.Close()
		t.Error("CreateDatagram returned a datagram")
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

type pipeStream struct{ net.Conn }

func (pipeStream) CloseWrite() error { return spec.ErrNoHalfClose }

// recordingDialer hands out one stream and remembers where it was asked to go.
type recordingDialer struct {
	serverStream spec.Stream
	dialed       chan spec.Destination
}

func (d recordingDialer) DialStream(_ context.Context, destination spec.Destination) (spec.Stream, error) {
	d.dialed <- destination
	return d.serverStream, nil
}

func (recordingDialer) Close() error { return nil }

type fakeDatagram struct{ spec.Datagram }

type fakeDatagramOverStreamClient struct {
	requested chan spec.Stream
	err       error
}

func (fakeDatagramOverStreamClient) DatagramDestinationIsWritten() spec.DatagramDestinationPlacement {
	return spec.DatagramDestinationInEveryDatagram
}

func (c fakeDatagramOverStreamClient) RequestDatagramOverStream(ctx context.Context, underlyingStream spec.Stream, _ spec.Metadata) (spec.Datagram, error) {
	c.requested <- underlyingStream
	if c.err != nil {
		return nil, c.err
	}
	return fakeDatagram{}, nil
}

// closeRecordingStream reports whether the outbound closed its server stream.
type closeRecordingStream struct {
	pipeStream
	closed chan struct{}
}

func (s closeRecordingStream) Close() error { close(s.closed); return nil }

func proxyServer(t *testing.T) spec.Destination {
	t.Helper()
	server, err := spec.NewDestinationFromDomain("proxy.example", 443)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

// With a datagram-over-stream client, the proxy outbound declares UDP before
// any packet, dials the proxy server -- never the packet's destination -- and
// hands that stream to the client.
func TestStreamOutboundCarriesUDPInsideAStream(t *testing.T) {
	serverStream := pipeStream{}
	dialer := recordingDialer{serverStream: serverStream, dialed: make(chan spec.Destination, 1)}
	client := fakeDatagramOverStreamClient{requested: make(chan spec.Stream, 1)}
	server := proxyServer(t)

	proxy := outbound.NewStream(outbound.StreamConfig{
		Tag: "proxy", ServerAddr: server, Dialer: dialer, DatagramOverStreamClient: client,
	})

	networks := proxy.Networks()
	if len(networks) != 2 || networks[0] != spec.NetworkTCP || networks[1] != spec.NetworkUDP {
		t.Errorf("Networks() = %v, want [tcp udp]", networks)
	}

	datagram, err := proxy.CreateDatagram(context.Background(), spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := datagram.(fakeDatagram); !ok {
		t.Errorf("CreateDatagram returned %T, want the client's datagram", datagram)
	}
	if dialed := <-dialer.dialed; dialed != server {
		t.Errorf("dialed %s, want the proxy server %s", dialed, server)
	}
	if handed := <-client.requested; handed != serverStream {
		t.Error("the client was not handed the dialed stream")
	}
}

// A refused request must not leak the connection to the server.
func TestAFailedDatagramRequestClosesTheServerStream(t *testing.T) {
	serverStream := closeRecordingStream{closed: make(chan struct{})}
	refusal := errors.New("refused")
	proxy := outbound.NewStream(outbound.StreamConfig{
		Tag:                      "proxy",
		ServerAddr:               proxyServer(t),
		Dialer:                   recordingDialer{serverStream: serverStream, dialed: make(chan spec.Destination, 1)},
		DatagramOverStreamClient: fakeDatagramOverStreamClient{requested: make(chan spec.Stream, 1), err: refusal},
	})

	_, err := proxy.CreateDatagram(context.Background(), spec.Metadata{Network: spec.NetworkUDP})
	if !errors.Is(err, refusal) {
		t.Errorf("CreateDatagram = %v, want the client's error", err)
	}
	select {
	case <-serverStream.closed:
	default:
		t.Error("the stream to the server was left open")
	}
}
