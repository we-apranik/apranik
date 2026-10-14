// Package informant implements component.RouterTrafficInformant for the
// handler: what the router asks about the traffic it is deciding on. Stream
// wraps one TCP connection for peeking only when a rule sniffs; UDPSession
// answers for one destination of a UDP session. Both resolve the destination's
// domain once, when a rule's resolve action asks.
//
// It lives apart from the router because the router never holds a stream;
// the router sees only the interface.
package informant
