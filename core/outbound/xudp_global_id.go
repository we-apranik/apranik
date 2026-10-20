package outbound

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"github.com/we-apranik/apranik/spec"
)

const xudpGlobalIDKeyLength = 32

// XUDPGlobalIDDeriver turns the source of a UDP session into its Global ID: the
// same source gives the same ID for as long as the deriver lives, so a client
// that reconnects names the session it had. The key is secret and random, so
// nobody can work out an ID from an address; Xray and Mihomo do the same with a
// per-process key. One deriver serves every outbound of a built core.
type XUDPGlobalIDDeriver struct {
	key [xudpGlobalIDKeyLength]byte
}

func NewXUDPGlobalIDDeriver() (*XUDPGlobalIDDeriver, error) {
	deriver := &XUDPGlobalIDDeriver{}
	if _, err := rand.Read(deriver.key[:]); err != nil {
		return nil, fmt.Errorf("outbound: making the XUDP Global ID key: %w", err)
	}
	return deriver, nil
}

func NewXUDPGlobalIDDeriverWithKey(key [xudpGlobalIDKeyLength]byte) *XUDPGlobalIDDeriver {
	return &XUDPGlobalIDDeriver{key: key}
}

func (d *XUDPGlobalIDDeriver) For(source spec.Endpoint) (globalID [8]byte) {
	if !source.IsValid() {
		return globalID
	}
	mac := hmac.New(sha256.New, d.key[:])
	mac.Write([]byte(source.String()))
	copy(globalID[:], mac.Sum(nil))
	return globalID
}
