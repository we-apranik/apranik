package tls

import (
	cryptotls "crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

var errNoCertificateInPEM = errors.New("no PEM certificate found")

func certificateAuthorityPool(authorities []CertificateAuthority) (*x509.CertPool, error) {
	if len(authorities) == 0 {
		return nil, nil
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	for index, authority := range authorities {
		pemBytes, source, err := readPEM(authority.CertificateFile, authority.Certificate)
		if err != nil {
			return nil, fmt.Errorf("certificate_authorities[%d]: %w", index, err)
		}
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("certificate_authorities[%d] (%s): %w", index, source, errNoCertificateInPEM)
		}
	}
	return pool, nil
}

// The key is never named in an error beyond where it came from.
func loadCertificates(certificates []Certificate) ([]cryptotls.Certificate, error) {
	loaded := make([]cryptotls.Certificate, 0, len(certificates))
	for index, certificate := range certificates {
		certificatePEM, certificateSource, err := readPEM(certificate.CertificateFile, certificate.Certificate)
		if err != nil {
			return nil, fmt.Errorf("certificates[%d]: %w", index, err)
		}
		keyPEM, keySource, err := readPEM(certificate.KeyFile, certificate.Key.Reveal())
		if err != nil {
			return nil, fmt.Errorf("certificates[%d]: %w", index, err)
		}
		pair, err := cryptotls.X509KeyPair(certificatePEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("certificates[%d] (%s, key %s): %w", index, certificateSource, keySource, err)
		}
		loaded = append(loaded, pair)
	}
	return loaded, nil
}

func readPEM(path, inline string) (pemBytes []byte, source string, err error) {
	if path == "" {
		return []byte(inline), "inline", nil
	}
	pemBytes, err = os.ReadFile(path)
	if err != nil {
		return nil, path, err
	}
	return pemBytes, path, nil
}
