# Your first module

A walk-through of the smallest module kind, a [sniffer](sniffer.md): from an empty
folder to a rule that acts on what it recognises. Every step was run as written.

The module recognises the line an SSH client sends first, such as
`SSH-2.0-OpenSSH_9.6`, so a routing rule can match `ssh`.

## 1. The folder

Create `module/sniffer/ssh/sniffer.go`:

```go
// Package ssh is a sniffer: it recognises the identification line an SSH
// client sends first ("SSH-2.0-OpenSSH_9.6") and reports the protocol as "ssh".
package ssh

import (
	"bytes"
	"errors"

	"github.com/we-apranik/apranik/spec"
)

var (
	errNotSSH            = errors.New("ssh: not an SSH identification line")
	identificationPrefix = []byte("SSH-")
)

type Settings struct{}

func (*Settings) Validate() error    { return nil }
func (*Settings) Warnings() []string { return nil }

type Sniffer struct{ Settings }

func (*Sniffer) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: "ssh", Description: "recognise an SSH client's identification line"}
}

func (*Sniffer) NewSniffer() (spec.Sniffer, error) { return identificationLineSniffer{}, nil }

func NewSnifferModule() spec.SnifferModule { return &Sniffer{} }

type identificationLineSniffer struct{}

func (identificationLineSniffer) Sniff(payloadPrefix []byte) (spec.SniffResult, error) {
	if len(payloadPrefix) < len(identificationPrefix) {
		if bytes.HasPrefix(identificationPrefix, payloadPrefix) {
			return spec.SniffResult{}, spec.ErrSniffNeedMoreData
		}
		return spec.SniffResult{}, errNotSSH
	}
	if !bytes.HasPrefix(payloadPrefix, identificationPrefix) {
		return spec.SniffResult{}, errNotSSH
	}
	return spec.SniffResult{Protocol: "ssh"}, nil
}
```

The module value is its own settings: `Validate` and `Warnings` are what the config
check calls. `Sniff` has three outcomes: a result, `spec.ErrSniffNeedMoreData` while
the bytes are only the start of the protocol, and any other error as soon as they
cannot be it.

## 2. The test

Tests run without core. Create `module/sniffer/ssh/sniffer_test.go`:

```go
package ssh

import (
	"errors"
	"testing"

	"github.com/we-apranik/apranik/spec"
)

func TestSniff(t *testing.T) {
	sniffer, err := NewSnifferModule().NewSniffer()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		prefix   string
		wantErr  error
		wantName string
	}{
		{"client line", "SSH-2.0-OpenSSH_9.6\r\n", nil, "ssh"},
		{"prefix only", "SS", spec.ErrSniffNeedMoreData, ""},
		{"empty", "", spec.ErrSniffNeedMoreData, ""},
		{"other protocol", "GET / HTTP/1.1\r\n", errNotSSH, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := sniffer.Sniff([]byte(test.prefix))
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if result.Protocol != test.wantName {
				t.Fatalf("protocol = %q, want %q", result.Protocol, test.wantName)
			}
		})
	}
}
```

```sh
go test -race ./module/sniffer/ssh
```

## 3. The registry line

In `core/registry/default.go`, import the package as `sshsniffer` and add one line
beside the other sniffers:

```go
moduleRegistry.Sniffers().Register(sshsniffer.NewSnifferModule),
```

That is the only file outside your folder. Build and see it listed:

```sh
go build -o apranik ./cmd/apranik
./apranik modules
```

## 4. Use it

A rule sniffs, and a later rule matches what was recognised. This one refuses SSH
that passes through the proxy:

```json
{
  "version": 1,
  "log": { "level": "info", "outputs": [{ "type": "stderr", "format": "human" }] },
  "inbounds": [
    {
      "tag": "local",
      "listen": "127.0.0.1",
      "port": 1080,
      "transport": [{ "type": "tcp" }],
      "protocol": { "type": "mixed", "settings": { "socks5": {}, "http": {} } }
    }
  ],
  "outbounds": [{ "tag": "direct", "protocol": { "type": "freedom" } }],
  "routing": {
    "rules": [
      {
        "match": { "inbound": ["local"] },
        "action": { "type": "sniff", "settings": { "sniffers": [{ "type": "ssh" }] } }
      },
      {
        "match": { "sniffed_protocol": ["ssh"] },
        "action": { "type": "block" }
      }
    ],
    "default_action": { "type": "route", "settings": { "outbound": "direct" } }
  }
}
```

```sh
./apranik check -config config.json
./apranik run -config config.json
```

Send a connection whose first bytes are `SSH-2.0-OpenSSH_9.6` through the proxy and
it is closed; anything else goes out through `freedom`. A rule that never sniffs
before it reads `sniffed_protocol` is warned about by `check`.

## What you did not touch

`spec/`, the builder, core's inbound and outbound, the config parser and every other
module. If a module you write needs one of them, stop and open an issue: the
contract is missing something general. Read [Pull requests](pull-requests.md) before
you send it, and the other module kinds ([transport](base-transport.md),
[protocol](proxy-protocol.md)) for what comes next.
