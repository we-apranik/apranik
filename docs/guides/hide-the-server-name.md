# Hide the server name

Keep the server's name out of the TLS handshake. Before you start, read [what these guides assume](index.md).

The same local proxy, with [`fragment`](../configuration/transport/fragment.md) under freedom:
each site's ClientHello leaves cut into pieces, which gets past DPI that
blocks by SNI without reassembling. No server is involved.

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
      "transport": [{ "type": "tcp" }, { "type": "fragment" }],
      "protocol": { "type": "freedom" }
    }
  ]
}
```

In a proxy client, put `fragment` directly below `tls` to hide the name used
to reach the server: `tcp → fragment → tls → ws`.
