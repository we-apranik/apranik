// Package outbound is where traffic leaves Apranik: through a proxy server, or
// straight to the destination with freedom. Blocking is a routing action, not
// an outbound. Each outbound states in advance
// which networks it can carry, so nothing is routed to it that it cannot serve.
//
// Nothing here bounds a dial or a handshake: every transport and protocol
// bounds its own, with the timeouts in its own settings.
package outbound
