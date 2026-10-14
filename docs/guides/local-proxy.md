# Run a local proxy with no server

A proxy on your own machine that needs no server. Before you start, read [what these guides assume](index.md).

Useful to try Apranik or to give apps one local port that sends everything
straight out. A [routing](../configuration/routing/index.md) section blocks ad servers, found by
their name even for connections that name an IP, and sends the rest out.

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
          "socks5": { "udp_relay": true },
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
  ],
  "datasets": [
    {
      "tag": "ads",
      "source": { "file": "geosite.dat" },
      "format": {
        "type": "geodata",
        "kind": "domain",
        "category": "category-ads-all"
      }
    }
  ],
  "routing": {
    "rules": [
      {
        "match": { "network": "tcp" },
        "action": {
          "type": "sniff",
          "settings": {
            "sniffers": [{ "type": "tls" }, { "type": "http" }]
          }
        }
      },
      {
        "match": { "domain_in_dataset": ["ads"] },
        "action": { "type": "block" }
      }
    ],
    "default_action": {
      "type": "route",
      "settings": { "outbound": "direct" }
    }
  }
}
```

`geosite.dat` sits next to the config file; any V2Ray or Xray `geosite.dat`
works. See [Datasets](../configuration/datasets.md).
