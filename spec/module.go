package spec

// ModuleInfo identifies the module, not the configured component: Info must
// return the same value for every value the constructor produces, independent
// of any setting. The registry checks this by constructing twice.
type ModuleInfo struct {
	Name        string // registry key: "tcp", "tls", "vless", "socks5"
	Description string // one line, for CLI listings and generated docs
}

// Config is what the configuration system decodes into and validates. The kind
// interfaces embed it, so the value the user configures and the component it
// builds are one thing. A module wanting its schema as a separate type embeds
// one: type Transport struct{ Settings }.
//
// Validate runs after decoding and before anything is built, so a bad
// configuration is refused at startup rather than on the first packet. It must
// not dial, listen, read files, or otherwise do work.
//
// Warnings reports advisory findings -- a setting that weakens security, a
// combination that will work but probably is not intended. It runs only when
// Validate returned nil, must not fail, and must not repeat what Validate
// already refused.
//
// Three rules the compiler cannot enforce:
//
//   - Do not put credential values in Validate's or Warnings' text. Use
//     types.Secret for passwords, UUIDs and keys, so %q and %v redact them.
//   - Treat the value as inert once Validate returns. Everything a New* or
//     Wrap* result needs must be derived from the settings alone and shared
//     with nothing else, because a module registered under several kinds is
//     constructed and decoded once per kind.
//   - Do not populate a map field with defaults. encoding/json MERGES into an
//     existing map rather than replacing it, so a default entry survives
//     decoding and the user has no way to remove it. Slices are safe: they are
//     replaced. Verified on Go 1.27.1.
type Config interface {
	Validate() error
	Warnings() []string
}

// ListenAddressWarner is optional on a module that runs in an inbound.
// config.Check calls it with the address that inbound listens on, so the
// module warns only about what that address makes unsafe. Warnings stays for
// what does not depend on it.
type ListenAddressWarner interface {
	WarningsForListenAddress(listen Endpoint) []string
}
