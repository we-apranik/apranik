# DNS

The resolver: how Apranik turns a domain into addresses. Every lookup goes through
it: freedom reaching a destination, a proxy outbound reaching its server, a DNS
server reaching its own hostname, and the routing [`resolve`](../routing/action.md#resolve)
action. Without a `dns` section, the operating system resolves.

[![How the resolver answers a lookup: an IP or localhost is answered at once; otherwise the DNS rules pick a server, the cache answers if it can, and on a miss the server's module sends one wire message over the server's own outbound. The answer is filtered by ip_versions, cached for its TTL and returned.](../../assets/diagrams/dns-lookup.excalidraw.svg)](../../assets/diagrams/dns-lookup.excalidraw.svg)

## Structure

<div class="cfg-structure">

```json
{
  "dns": {
    "servers": [
      {
        "tag": "google",
        "type": "doh",
        "settings": { "server": "8.8.8.8", "host": "dns.google" },
        "tls": { "server_name": "dns.google" },
        "outbound": "proxy"
      },
      {
        "tag": "domestic",
        "type": "udp",
        "settings": { "server": "192.0.2.53" },
        "outbound": "direct"
      }
    ],
    "rules": [
      {
        "match": { "domain_suffix": ["ir"] },
        "server": "domestic"
      }
    ],
    "default_server": "google",
    "ip_versions": "prefer_ipv4",
    "cache": {
      "disable": false,
      "max_entries": 4096,
      "per_server": false,
      "serve_stale": { "enabled": false, "max_stale_age": "1h" }
    }
  }
}
```

</div>

<div class="cfg-fields">

## Fields

### `servers`

<span class="cfg cfg-type">list of DNS servers</span>
<span class="cfg cfg-req">Required</span>

The servers lookups may ask. See [DNS server](server.md).

### `rules`

<span class="cfg cfg-type">list of DNS rules</span>

Which server answers which names; the first match wins. See [DNS rule](rule.md).

### `default_server`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

The tag of the server for names no rule matches.

### `ip_versions`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>prefer_ipv4</code></span>

Which addresses a lookup asks for, and in what order:

| Value | Returns |
| --- | --- |
| `ipv4_only` | IPv4 only |
| `ipv6_only` | IPv6 only |
| `prefer_ipv4` | IPv4 first, then IPv6 |
| `prefer_ipv6` | IPv6 first, then IPv4 |

### `cache.disable`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>false</code></span>

Ask the server every time. Otherwise answers are kept for their TTL, and a name
that does not exist for at most 5 minutes. Failures are never kept.

### `cache.max_entries`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: <code>4096</code></span>

The most answers kept; the least recently used goes first. Each record type of a
name is one answer.

### `cache.per_server`

<span class="cfg cfg-type">boolean</span>
<span class="cfg cfg-def">Default: <code>false</code></span>

One cache per server instead of one shared cache.

### `cache.serve_stale`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-def">Default: off</span>

`enabled` returns an expired answer at once and refreshes it in the background,
while it is no older than `max_stale_age` (default `1h`) past expiry (RFC 8767).
It keeps connections working while a server is briefly unreachable.

</div>

## Example: avoiding a lookup loop

A proxy whose `server` is a domain needs a DNS server that does not go through that
proxy. Here the proxy's own name comes from the domestic resolver:

```json
{
  "outbounds": [
    {
      "tag": "proxy",
      "server": "proxy.example.com",
      "server_port": 443,
      "transport": [{ "type": "tcp" }, { "type": "tls" }],
      "protocol": {
        "type": "trojan",
        "settings": { "password": "correct horse" }
      },
      "dns_server": "domestic"
    },
    {
      "tag": "direct",
      "protocol": { "type": "freedom" }
    }
  ],
  "dns": {
    "servers": [
      {
        "tag": "google",
        "type": "doh",
        "settings": { "server": "8.8.8.8", "host": "dns.google" },
        "outbound": "proxy"
      },
      {
        "tag": "domestic",
        "type": "udp",
        "settings": { "server": "192.0.2.53" },
        "outbound": "direct"
      }
    ],
    "default_server": "google"
  }
}
```

`apranik check` refuses a loop it can see and names every server and outbound in it.
