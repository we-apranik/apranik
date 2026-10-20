package tls

import (
	cryptotls "crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/spec"
)

type Settings struct {
	ServerName string `json:"server_name"`

	Insecure bool `json:"insecure"`

	CertificateAuthorities []CertificateAuthority `json:"certificate_authorities"`

	Certificates []Certificate `json:"certificates"`

	ALPN []string `json:"alpn"`

	MinVersion string `json:"min_version"`
	MaxVersion string `json:"max_version"`

	Fingerprint string `json:"fingerprint"`

	HandshakeTimeout *types.Duration `json:"handshake_timeout"`
}

// PROVISIONAL.
const defaultHandshakeTimeout = 10 * time.Second

func (s *Settings) handshakeTimeout() time.Duration {
	return time.Duration(types.ValueOrDefault(s.HandshakeTimeout, types.Duration(defaultHandshakeTimeout)))
}

type CertificateAuthority struct {
	CertificateFile string `json:"certificate_file"`
	Certificate     string `json:"certificate"`
}

type Certificate struct {
	CertificateFile string       `json:"certificate_file"`
	Certificate     string       `json:"certificate"`
	KeyFile         string       `json:"key_file"`
	Key             types.Secret `json:"key"`
}

var versionsByName = map[string]uint16{
	"":    0,
	"1.0": cryptotls.VersionTLS10,
	"1.1": cryptotls.VersionTLS11,
	"1.2": cryptotls.VersionTLS12,
	"1.3": cryptotls.VersionTLS13,
}

const maxALPNProtocolLength = 255

func (s *Settings) Validate() error {
	if err := types.CheckPositive(s.HandshakeTimeout); err != nil {
		return fmt.Errorf("tls: handshake_timeout %w", err)
	}
	minVersion, known := versionsByName[s.MinVersion]
	if !known {
		return fmt.Errorf("tls: min_version %q is not one of 1.0, 1.1, 1.2, 1.3", s.MinVersion)
	}
	maxVersion, known := versionsByName[s.MaxVersion]
	if !known {
		return fmt.Errorf("tls: max_version %q is not one of 1.0, 1.1, 1.2, 1.3", s.MaxVersion)
	}
	if minVersion != 0 && maxVersion != 0 && minVersion > maxVersion {
		return fmt.Errorf("tls: min_version %s is above max_version %s", s.MinVersion, s.MaxVersion)
	}

	if !isKnownFingerprint(s.Fingerprint) {
		return fmt.Errorf("tls: fingerprint %q is not known; see the tls page for the list", s.Fingerprint)
	}
	if s.usesFingerprint() && (s.MinVersion != "" || s.MaxVersion != "") {
		return fmt.Errorf("tls: min_version and max_version cannot be used with fingerprint %q, whose ClientHello fixes the versions; set fingerprint to %q to use them", s.Fingerprint, fingerprintNone)
	}

	for index, protocol := range s.ALPN {
		if protocol == "" || len(protocol) > maxALPNProtocolLength {
			return fmt.Errorf("tls: alpn[%d] must be 1 to %d bytes", index, maxALPNProtocolLength)
		}
	}

	for index, authority := range s.CertificateAuthorities {
		if err := exactlyOne(authority.CertificateFile != "", authority.Certificate != ""); err != nil {
			return fmt.Errorf("tls: certificate_authorities[%d]: set certificate_file or certificate: %w", index, err)
		}
	}

	for index, certificate := range s.Certificates {
		if err := exactlyOne(certificate.CertificateFile != "", certificate.Certificate != ""); err != nil {
			return fmt.Errorf("tls: certificates[%d]: set certificate_file or certificate: %w", index, err)
		}
		if err := exactlyOne(certificate.KeyFile != "", certificate.Key != ""); err != nil {
			return fmt.Errorf("tls: certificates[%d]: set key_file or key: %w", index, err)
		}
	}
	return nil
}

var (
	errNeitherSet = errors.New("neither is set")
	errBothSet    = errors.New("both are set")
)

func exactlyOne(first, second bool) error {
	switch {
	case !first && !second:
		return errNeitherSet
	case first && second:
		return errBothSet
	}
	return nil
}

func (s *Settings) Warnings() []string {
	var warnings []string
	if s.Insecure {
		warnings = append(warnings, "tls: insecure is on, so the server's certificate is not checked and anyone on the path can read and change the traffic")
		if len(s.CertificateAuthorities) > 0 {
			warnings = append(warnings, "tls: certificate_authorities has no effect while insecure is on")
		}
	}
	if s.MinVersion != "" && versionsByName[s.MinVersion] < cryptotls.VersionTLS12 {
		warnings = append(warnings, fmt.Sprintf("tls: min_version %s allows protocol versions with known weaknesses; 1.2 is the usual floor", s.MinVersion))
	}
	if fingerprint, known := fingerprintsByName[s.Fingerprint]; known && fingerprint.stale {
		warnings = append(warnings, fmt.Sprintf("tls: fingerprint %q imitates %s, which is rarely seen today and may stand out; prefer chrome, firefox or safari", s.Fingerprint, fingerprint.imitates))
	}
	return warnings
}

// The same settings serve both roles, and nothing tells them which. A server
// cannot run without certificates and a client never reads them, so settings
// without certificates are taken to be a client's: the default fingerprint then
// applies, and check can see what it will offer.
func (s *Settings) looksLikeAClient() bool {
	return len(s.Certificates) == 0
}

func (s *Settings) usesFingerprint() bool {
	if s.Fingerprint == "" {
		return s.looksLikeAClient()
	}
	return s.Fingerprint != fingerprintNone
}

func (s *Settings) TransportInfo() spec.TransportInfo {
	if len(s.ALPN) > 0 {
		return spec.TransportInfo{ApplicationProtocols: s.ALPN}
	}
	if s.usesFingerprint() {
		return spec.TransportInfo{ApplicationProtocols: presetApplicationProtocols(effectiveFingerprint(s.Fingerprint))}
	}
	return spec.TransportInfo{}
}
