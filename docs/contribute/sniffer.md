# Add a sniffer

The examples are `module/sniffer/tls` and `module/sniffer/http`. The module
value implements `spec.SnifferModule` (settings, `Info`, and `NewSniffer()`,
which prepares anything needed once), and the value it builds implements
`spec.Sniffer`. `Sniff` is called from many connections at once:

- return a `spec.SniffResult` when the bytes are recognised;
- return `spec.ErrSniffNeedMoreData` while they are only a prefix of your protocol;
- return any other error as soon as they cannot be your protocol, so a connection
  is not held waiting;
- only trim what you read; do not lowercase or otherwise rewrite it.

Register it with `moduleRegistry.Sniffers().Register(<name>.NewSnifferModule)`.
