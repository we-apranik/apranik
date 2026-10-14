package tls

import (
	"io"
	"net"
	"testing"

	utls "github.com/refraction-networking/utls"

	"github.com/we-apranik/apranik/spec"
)

func TestBrowserFamilyOfEveryUTLSPreset(t *testing.T) {
	cases := []struct {
		name          string
		helloID       utls.ClientHelloID
		browserFamily string
	}{
		{"chrome auto", utls.HelloChrome_Auto, spec.BrowserFamilyChromium},
		{"chrome 58", utls.HelloChrome_58, spec.BrowserFamilyChromium},
		{"chrome 70", utls.HelloChrome_70, spec.BrowserFamilyChromium},
		{"chrome 83", utls.HelloChrome_83, spec.BrowserFamilyChromium},
		{"chrome 100", utls.HelloChrome_100, spec.BrowserFamilyChromium},
		{"chrome 120", utls.HelloChrome_120, spec.BrowserFamilyChromium},
		{"chrome 131", utls.HelloChrome_131, spec.BrowserFamilyChromium},
		{"edge", utls.HelloEdge_Auto, spec.BrowserFamilyChromium},
		{"qq, built on chromium", utls.HelloQQ_Auto, spec.BrowserFamilyChromium},
		{"firefox auto", utls.HelloFirefox_Auto, spec.BrowserFamilyFirefox},
		{"firefox 65", utls.HelloFirefox_65, spec.BrowserFamilyFirefox},
		{"firefox 99", utls.HelloFirefox_99, spec.BrowserFamilyFirefox},
		{"firefox 105", utls.HelloFirefox_105, spec.BrowserFamilyFirefox},
		{"safari auto", utls.HelloSafari_Auto, spec.BrowserFamilySafari},
		{"ios auto", utls.HelloIOS_Auto, spec.BrowserFamilySafari},
		{"firefox 55, before record_size_limit", utls.HelloFirefox_55, ""},
		{"ios 12, before GREASE", utls.HelloIOS_12_1, ""},
		{"ios 13, before GREASE", utls.HelloIOS_13, ""},
		{"360, without GREASE", utls.Hello360_Auto, ""},
		{"okhttp", utls.HelloAndroid_11_OkHttp, ""},
		{"go", utls.HelloGolang, ""},
	}
	sniffer := clientHelloSniffer{}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := sniffer.Sniff(clientHelloFromPreset(t, testCase.helloID))
			if err != nil {
				t.Fatalf("Sniff: %v", err)
			}
			if result.Domain != "example.com" {
				t.Errorf("Domain = %q, want example.com", result.Domain)
			}
			if result.BrowserFamily != testCase.browserFamily {
				t.Errorf("BrowserFamily = %q, want %q", result.BrowserFamily, testCase.browserFamily)
			}
		})
	}
}

// The first flight a uTLS client writes, which is one record carrying the
// whole ClientHello for every preset.
func clientHelloFromPreset(t *testing.T, helloID utls.ClientHelloID) []byte {
	t.Helper()
	clientEnd, serverEnd := net.Pipe()
	firstFlight := make(chan []byte, 1)
	go func() {
		buffer := make([]byte, 1<<16)
		header := buffer[:5]
		if _, err := io.ReadFull(serverEnd, header); err != nil {
			firstFlight <- nil
			return
		}
		bodySize := int(header[3])<<8 | int(header[4])
		if _, err := io.ReadFull(serverEnd, buffer[5:5+bodySize]); err != nil {
			firstFlight <- nil
			return
		}
		firstFlight <- buffer[:5+bodySize]
		serverEnd.Close()
	}()
	client := utls.UClient(clientEnd, &utls.Config{ServerName: "example.com", InsecureSkipVerify: true}, helloID)
	client.Handshake()
	clientEnd.Close()
	record := <-firstFlight
	if record == nil {
		t.Fatal("no ClientHello was written")
	}
	return record
}
