package tls

import (
	cryptotls "crypto/tls"
	"errors"
	"fmt"

	"github.com/The-Apranik/Apranik/config/types"
)

// One settings type for both roles: an outbound reads the client fields, an
// inbound the server ones, and each ignores the other's.
type Settings struct {
	// Client: the name sent as SNI and checked against the server's
	// certificate. Empty means the server's domain; for a server given as an
	// IP, no SNI is sent and the certificate must carry that IP.
	ServerName string `json:"server_name"`

	// Client: accept any certificate. Anyone on the path can then read and
	// change the traffic.
	Insecure bool `json:"insecure"`

	// Client: trusted in addition to the system's roots.
	CertificateAuthorities []CertificateAuthority `json:"certificate_authorities"`

	// Server: at least one is required. With several, the one matching the
	// client's SNI is used.
	Certificates []Certificate `json:"certificates"`

	// Both: application protocols offered (client) or accepted (server), in
	// preference order. A server with a list refuses a client whose offer
	// shares nothing with it.
	ALPN []string `json:"alpn"`

	MinVersion string `json:"min_version"`
	MaxVersion string `json:"max_version"`
}

// A PEM certificate, from a file or inline: exactly one of the two.
type CertificateAuthority struct {
	CertificateFile string `json:"certificate_file"`
	Certificate     string `json:"certificate"`
}

// A PEM certificate chain and its private key, each from a file or inline.
type Certificate struct {
	CertificateFile string       `json:"certificate_file"`
	Certificate     string       `json:"certificate"`
	KeyFile         string       `json:"key_file"`
	Key             types.Secret `json:"key"`
}

var versionsByName = map[string]uint16{
	"1.0": cryptotls.VersionTLS10,
	"1.1": cryptotls.VersionTLS11,
	"1.2": cryptotls.VersionTLS12,
	"1.3": cryptotls.VersionTLS13,
}

const maxALPNProtocolLength = 255

func (s *Settings) Validate() error {
	minVersion, known := versionsByName[s.MinVersion]
	if !known {
		return fmt.Errorf("tls: min_version %q is not one of 1.0, 1.1, 1.2, 1.3", s.MinVersion)
	}
	maxVersion, known := versionsByName[s.MaxVersion]
	if !known {
		return fmt.Errorf("tls: max_version %q is not one of 1.0, 1.1, 1.2, 1.3", s.MaxVersion)
	}
	if minVersion > maxVersion {
		return fmt.Errorf("tls: min_version %s is above max_version %s", s.MinVersion, s.MaxVersion)
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
	if versionsByName[s.MinVersion] < cryptotls.VersionTLS12 {
		warnings = append(warnings, fmt.Sprintf("tls: min_version %s allows protocol versions with known weaknesses; 1.2 is the usual floor", s.MinVersion))
	}
	return warnings
}
