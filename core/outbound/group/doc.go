// Package group holds the outbound groups: outbounds core builds that choose
// one of their members, other outbounds by tag, for each connection. urltest
// prefers the fastest member and fallback the first that works, both from a
// background probe; selector uses the member a person picked. The members, the
// probe and its defaults are shared; each group type is its own struct.
//
// A group is a component.Outbound like any other. It lives apart from
// package outbound, which holds the proxy outbound, freedom and the delay
// test the groups' probes are built on.
package group
