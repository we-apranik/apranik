package tls

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"slices"

	utls "github.com/refraction-networking/utls"
)

const (
	// Unset means this, as Xray has since 2024-12: Go's own ClientHello is
	// shared by every Go program and easy to single out.
	defaultFingerprint = "chrome"
	// Go's crypto/tls, as before fingerprints existed.
	fingerprintNone = "none"
	// Chosen once per client from the modern presets, as sing-box does.
	fingerprintRandom = "random"
	// A made-up ClientHello, seeded once per client.
	fingerprintRandomized = "randomized"
)

type fingerprint struct {
	helloID  utls.ClientHelloID
	imitates string
	// Older than the browser's newest preset in uTLS: rarely seen today, so
	// it may stand out rather than blend in.
	stale bool
}

// The short names are the ones Xray and sing-box share; the versioned ones
// are uTLS's own Go names, lowercased, as Xray spells them. Reviewed at each
// uTLS upgrade. Left out: uTLS's beta variants (_PSK, _PQ, _Shuffle) and the
// two presets uTLS itself calls incompatible (Edge 106, 360 11.0).
var fingerprintsByName = map[string]fingerprint{
	"chrome":  {helloID: utls.HelloChrome_Auto, imitates: "Chrome"},
	"firefox": {helloID: utls.HelloFirefox_Auto, imitates: "Firefox"},
	"safari":  {helloID: utls.HelloSafari_Auto, imitates: "Safari"},
	"ios":     {helloID: utls.HelloIOS_Auto, imitates: "Safari on iOS 14", stale: true},
	"android": {helloID: utls.HelloAndroid_11_OkHttp, imitates: "OkHttp on Android 11", stale: true},
	"edge":    {helloID: utls.HelloEdge_Auto, imitates: "Edge 85", stale: true},
	"360":     {helloID: utls.Hello360_Auto, imitates: "360 Browser 7.5", stale: true},
	"qq":      {helloID: utls.HelloQQ_Auto, imitates: "QQ Browser 11.1", stale: true},

	"hellochrome_58":         {helloID: utls.HelloChrome_58, imitates: "Chrome 58", stale: true},
	"hellochrome_62":         {helloID: utls.HelloChrome_62, imitates: "Chrome 62", stale: true},
	"hellochrome_70":         {helloID: utls.HelloChrome_70, imitates: "Chrome 70", stale: true},
	"hellochrome_72":         {helloID: utls.HelloChrome_72, imitates: "Chrome 72", stale: true},
	"hellochrome_83":         {helloID: utls.HelloChrome_83, imitates: "Chrome 83", stale: true},
	"hellochrome_87":         {helloID: utls.HelloChrome_87, imitates: "Chrome 87", stale: true},
	"hellochrome_96":         {helloID: utls.HelloChrome_96, imitates: "Chrome 96", stale: true},
	"hellochrome_100":        {helloID: utls.HelloChrome_100, imitates: "Chrome 100", stale: true},
	"hellochrome_102":        {helloID: utls.HelloChrome_102, imitates: "Chrome 102", stale: true},
	"hellochrome_120":        {helloID: utls.HelloChrome_120, imitates: "Chrome 120", stale: true},
	"hellochrome_131":        {helloID: utls.HelloChrome_131, imitates: "Chrome 131", stale: true},
	"hellochrome_133":        {helloID: utls.HelloChrome_133, imitates: "Chrome 133"},
	"hellofirefox_55":        {helloID: utls.HelloFirefox_55, imitates: "Firefox 55", stale: true},
	"hellofirefox_56":        {helloID: utls.HelloFirefox_56, imitates: "Firefox 56", stale: true},
	"hellofirefox_63":        {helloID: utls.HelloFirefox_63, imitates: "Firefox 63", stale: true},
	"hellofirefox_65":        {helloID: utls.HelloFirefox_65, imitates: "Firefox 65", stale: true},
	"hellofirefox_99":        {helloID: utls.HelloFirefox_99, imitates: "Firefox 99", stale: true},
	"hellofirefox_102":       {helloID: utls.HelloFirefox_102, imitates: "Firefox 102", stale: true},
	"hellofirefox_105":       {helloID: utls.HelloFirefox_105, imitates: "Firefox 105", stale: true},
	"hellofirefox_120":       {helloID: utls.HelloFirefox_120, imitates: "Firefox 120"},
	"hellosafari_16_0":       {helloID: utls.HelloSafari_16_0, imitates: "Safari 16.0"},
	"helloios_11_1":          {helloID: utls.HelloIOS_11_1, imitates: "Safari on iOS 11.1", stale: true},
	"helloios_12_1":          {helloID: utls.HelloIOS_12_1, imitates: "Safari on iOS 12.1", stale: true},
	"helloios_13":            {helloID: utls.HelloIOS_13, imitates: "Safari on iOS 13", stale: true},
	"helloios_14":            {helloID: utls.HelloIOS_14, imitates: "Safari on iOS 14", stale: true},
	"helloandroid_11_okhttp": {helloID: utls.HelloAndroid_11_OkHttp, imitates: "OkHttp on Android 11", stale: true},
	"helloedge_85":           {helloID: utls.HelloEdge_85, imitates: "Edge 85", stale: true},
	"hello360_7_5":           {helloID: utls.Hello360_7_5, imitates: "360 Browser 7.5", stale: true},
	"helloqq_11_1":           {helloID: utls.HelloQQ_11_1, imitates: "QQ Browser 11.1", stale: true},
}

// sing-box's list for "random".
var modernFingerprintNames = []string{"chrome", "firefox", "edge", "safari", "ios"}

// What a random or randomized ClientHello may offer, so the config check
// assumes the worst: its list is not known until the client is built.
var randomApplicationProtocols = []string{"h2", "http/1.1"}

func isKnownFingerprint(name string) bool {
	switch name {
	case "", fingerprintNone, fingerprintRandom, fingerprintRandomized:
		return true
	}
	_, known := fingerprintsByName[name]
	return known
}

// effectiveFingerprint is the name a client uses: unset means the default.
func effectiveFingerprint(name string) string {
	if name == "" {
		return defaultFingerprint
	}
	return name
}

// resolveClientHelloID turns a fingerprint name into the ClientHello a client
// sends on every connection. random and randomized are decided here, once.
func resolveClientHelloID(name string) (utls.ClientHelloID, error) {
	switch name {
	case fingerprintRandom:
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(modernFingerprintNames))))
		if err != nil {
			return utls.ClientHelloID{}, fmt.Errorf("choosing a random fingerprint: %w", err)
		}
		return fingerprintsByName[modernFingerprintNames[index.Int64()]].helloID, nil

	case fingerprintRandomized:
		seed, err := utls.NewPRNGSeed()
		if err != nil {
			return utls.ClientHelloID{}, fmt.Errorf("seeding the randomized fingerprint: %w", err)
		}
		// As Xray and sing-box weigh it: always offer TLS 1.3, never lead
		// with a P-256 key share.
		weights := utls.DefaultWeights
		weights.TLSVersMax_Set_VersionTLS13 = 1
		weights.FirstKeyShare_Set_CurveP256 = 0
		helloID := utls.HelloRandomizedALPN
		helloID.Seed = seed
		helloID.Weights = &weights
		return helloID, nil
	}
	return fingerprintsByName[name].helloID, nil
}

// presetApplicationProtocols is the ALPN list a fingerprint offers when alpn
// is unset, read from the uTLS preset without making a connection.
func presetApplicationProtocols(name string) []string {
	switch name {
	case fingerprintNone:
		return nil
	case fingerprintRandom, fingerprintRandomized:
		return randomApplicationProtocols
	}
	helloSpec, err := utls.UTLSIdToSpec(fingerprintsByName[name].helloID)
	if err != nil {
		return randomApplicationProtocols
	}
	for _, extension := range helloSpec.Extensions {
		if alpn, isALPN := extension.(*utls.ALPNExtension); isALPN {
			return alpn.AlpnProtocols
		}
	}
	return nil
}

// replaceApplicationProtocols puts protocols in place of the preset's ALPN
// list, leaving every other extension as the preset built it. A preset with
// no ALPN extension gets one, before its padding if it has any, since padding
// is sized to the extensions ahead of it.
func replaceApplicationProtocols(connection *utls.UConn, protocols []string) {
	for _, extension := range connection.Extensions {
		if alpn, isALPN := extension.(*utls.ALPNExtension); isALPN {
			alpn.AlpnProtocols = protocols
			return
		}
	}
	added := &utls.ALPNExtension{AlpnProtocols: protocols}
	for index, extension := range connection.Extensions {
		if _, isPadding := extension.(*utls.UtlsPaddingExtension); isPadding {
			connection.Extensions = append(connection.Extensions[:index], append([]utls.TLSExtension{added}, connection.Extensions[index:]...)...)
			return
		}
	}
	connection.Extensions = append(connection.Extensions, added)
}

// A randomized ClientHello can list X25519MLKEM768 without a key share for
// it. A Go server prefers that group and asks for it in a HelloRetryRequest,
// which uTLS cannot answer for a hybrid group, so the handshake fails: every
// time, since the seed is fixed per client. Unlisting the group keeps the
// ClientHello consistent; the browser presets all send its key share and are
// left alone.
func dropHybridGroupWithoutKeyShare(connection *utls.UConn) (changed bool) {
	var curves *utls.SupportedCurvesExtension
	hasHybridKeyShare := false
	for _, extension := range connection.Extensions {
		switch typed := extension.(type) {
		case *utls.SupportedCurvesExtension:
			curves = typed
		case *utls.KeyShareExtension:
			for _, share := range typed.KeyShares {
				if share.Group == utls.X25519MLKEM768 {
					hasHybridKeyShare = true
				}
			}
		}
	}
	if curves == nil || hasHybridKeyShare || !slices.Contains(curves.Curves, utls.X25519MLKEM768) {
		return false
	}
	curves.Curves = slices.DeleteFunc(slices.Clone(curves.Curves), func(curve utls.CurveID) bool {
		return curve == utls.X25519MLKEM768
	})
	return true
}
