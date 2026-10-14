# First configuration

A local proxy with no server, to see Apranik work. It listens on `127.0.0.1:1080`
for both SOCKS5 and HTTP, and sends everything straight out. Save this as
`config.json`:

```json
{
  "version": 1,
  "log": {
    "level": "info",
    "outputs": [{ "type": "stderr", "format": "human" }]
  },
  "inbounds": [
    {
      "tag": "local",
      "listen": "127.0.0.1",
      "port": 1080,
      "network": "tcp",
      "transport": [{ "type": "tcp" }],
      "protocol": {
        "type": "mixed",
        "settings": {
          "socks5": {},
          "http": {}
        }
      }
    }
  ],
  "outbounds": [
    {
      "tag": "direct",
      "protocol": { "type": "freedom" }
    }
  ]
}
```

Check it, then run it:

```sh
./apranik check -config config.json
./apranik run -config config.json
```

`check` reads the file and builds everything without opening a port, and lists every
problem at once with its location. `run` prints `Apranik is running` when the port is open.

In another terminal, send a request through it:

```sh
curl --socks5-hostname 127.0.0.1:1080 https://example.com/
curl -x http://127.0.0.1:1080 https://example.com/
```

Stop it with `Ctrl+C`; it closes its connections and exits.

[![A request through the first configuration: an app sends SOCKS5 or HTTP to the local inbound on 127.0.0.1:1080, which tells the two apart by the first byte. With no routing section the first outbound is the default, freedom, which connects straight to the destination.](../assets/diagrams/first-configuration.excalidraw.svg)](../assets/diagrams/first-configuration.excalidraw.svg)

## What each part is

- An **inbound** accepts connections. This one is a `tcp` listener, with the
  `mixed` protocol, which tells SOCKS5 from HTTP by the first byte.
- An **outbound** carries them onward. `freedom` connects straight to each destination.
- With no `routing` section, the first outbound is the default.

This port has no `users`, so every app on the machine can use it. Keep it on
`127.0.0.1`, or add [users](../configuration/inbound/users.md).

## Where next

- Reach a server outside the network: [Run Trojan over TLS](../guides/trojan-over-tls.md).
- Choose what goes where: [routing](../configuration/routing/index.md).
- Every top-level key: [configuration file](../configuration/index.md).
