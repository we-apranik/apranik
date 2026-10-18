# Run Trojan over TLS

The simplest setup: a server that looks like an HTTPS site, and a client that
reaches it. A request with a wrong password goes to `fallback`, such as a web
server on the same machine, so a prober sees an ordinary site. Before you start,
read [what these guides assume](index.md).

:::tabs
== Server

```json [server.json]
{
  "version": 1,
  "log": {
    "level": "info",
    "outputs": [{ "type": "stderr" }]
  },
  "inbounds": [
    {
      "tag": "trojan-in",
      "listen": "0.0.0.0",
      "port": 443,
      "network": "tcp",
      "transport": [
        { "type": "tcp" },
        {
          "type": "tls",
          "settings": {
            "certificates": [
              {
                "certificate_file": "/etc/apranik/fullchain.pem",
                "key_file": "/etc/apranik/privkey.pem"
              }
            ],
            "alpn": ["http/1.1"]
          }
        }
      ],
      "protocol": {
        "type": "trojan",
        "settings": {
          "users": [{ "name": "alice", "password": "change me" }]
        }
      },
      "fallback": { "server": "127.0.0.1", "server_port": 8080 }
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

== Client

```json [client.json]
{
  "version": 1,
  "log": {
    "level": "info",
    "outputs": [{ "type": "stderr" }]
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
          "socks5": { "udp_relay": true },
          "http": {}
        }
      }
    }
  ],
  "outbounds": [
    {
      "tag": "proxy",
      "server": "proxy.example.com",
      "server_port": 443,
      "transport": [
        { "type": "tcp" },
        {
          "type": "tls",
          "settings": { "alpn": ["http/1.1"] }
        }
      ],
      "protocol": {
        "type": "trojan",
        "settings": { "password": "change me" }
      }
    }
  ]
}
```

:::

The client's `tls` sends `proxy.example.com` as the SNI, checks the certificate
against the system's roots, and sends Chrome's ClientHello. For a self-signed
certificate, add it to `certificate_authorities` instead of turning on `insecure`.
