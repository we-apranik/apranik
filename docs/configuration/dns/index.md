# DNS

The `dns` section configures the **resolver**: the part of Apranik that turns a
domain into addresses. Every lookup goes through it: freedom dialing a
destination, a proxy outbound reaching its own server, a DNS server reaching
its resolver, and the routing [`resolve`](../routing/index.md#resolve) action.

Without a `dns` section, the operating system resolves, as before the section
existed, with a 5 second timeout and the [cache](#cache) defaults.

The plain system resolver on many networks, Iran's among them, is poisoned:
blocked names get false answers, and every query shows which sites are being
visited. A `dns` section sends queries where you choose: DNS over HTTPS or TLS,
through a fragmenting freedom or through the proxy itself.

The design is a design note.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "dns": {
    "ip_versions": "prefer_ipv4",
    "cache": {
      "disable": false,
      "max_entries": 4096,
      "per_server": false,
      "serve_stale": { "enabled": false, "max_stale_age": "1h" }
    },
    "servers": [
      {
        "tag": "google",
        "type": "doh",
        "settings": { "server": "8.8.8.8", "host": "dns.google" },
        "tls": { "server_name": "dns.google" },
        "outbound": "proxy"
      },
      { "tag": "domestic", "type": "udp", "settings": { "server": "192.0.2.53" }, "outbound": "direct" },
      { "tag": "os", "type": "system" }
    ],
    "rules": [
      { "match": { "domain_in_dataset": ["ir-domains"] }, "server": "domestic" }
    ],
    "default_server": "google"
  }
}
```

</div>

A lookup goes through these steps:

1. A name that is an IP, or `localhost` and the names under it, is answered at
   once and never sent anywhere.
2. The [`rules`](#rules) are checked in order; the first that matches picks the
   server, and `default_server` answers every name no rule matched.
3. The [cache](#cache) answers if it can.
4. Otherwise the server is asked for the record types
   [`ip_versions`](#ip_versions) needs, and the answer is cached for its TTL.

<div class="cfg-fields" markdown>

## Fields

### `servers`

<span class="cfg cfg-type">list of servers</span>
<span class="cfg cfg-req">Required</span>

The DNS servers lookups may ask. See [Servers](#servers_1). At least one.

### `rules`

<span class="cfg cfg-type">list of rules</span>

Which server answers which names. See [Rules](#rules_1).

### `default_server`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

The tag of the server that answers every name no rule matches. It also
resolves a proxy outbound's own `server` name and a DNS server's own hostname
when they name no [`dns_server`](#dns_server).

### `ip_versions`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>prefer_ipv4</code></span>

Which addresses a lookup asks for, and in what order they come back. A server
may override it.

| Value | Asks for | Returns |
| --- | --- | --- |
| `ipv4_only` | `A` | IPv4 only |
| `ipv6_only` | `AAAA` | IPv6 only |
| `prefer_ipv4` | both, at once | IPv4 first, then IPv6 |
| `prefer_ipv6` | both, at once | IPv6 first, then IPv4 |

On a network without working IPv6, `ipv4_only` saves a query per name. With
both, [`tcp`](../transport/tcp.md#next_address_delay) tries the addresses in
this order, the next one starting beside a slow one, so a dead IPv6 route
costs a quarter of a second, not the whole dial.

### `cache`

<span class="cfg cfg-type">object</span>

Answers are kept for their TTL: the number of seconds the DNS server says an
answer may be reused. A name that does not exist is kept too, for as long as
the server's answer allows and at most 5 minutes. A failure, such as a
timeout, is never kept. When several connections ask for the same name at
once, one query is sent and all of them wait for it.

- **`disable`** (boolean, default `false`): ask the server every time.
- **`max_entries`** (integer, default `4096`): the most answers kept. When the
  cache is full, the answer used least recently is dropped. An answer is a
  name and one record type: `example.com` with both IPv4 and IPv6 is two.
- **`per_server`** (boolean, default `false`): a separate cache for each
  server, each with its own `max_entries`. By default one cache serves all of
  them: a name is always sent to the same server by the rules, so they cannot
  disagree. A lookup that names its server (a `dns_server`, or a `resolve`
  with a `server`) is cached apart either way, so it never receives another
  server's answer.
- **`serve_stale`** (object): `{"enabled": true, "max_stale_age": "1h"}`
  returns an expired answer at once and refreshes it in the background, while
  it is no older than `max_stale_age` past its expiry (RFC 8767). It keeps
  connections working while a DNS server is slow or briefly unreachable, at
  the price of sometimes using an address that has just changed. Off by
  default; `max_stale_age` defaults to `1h`.

</div>

## Servers

A server is a `type` and its `settings`, like a transport or a protocol, and
beside them the fields core reads.

<div class="cfg-structure" markdown>

```json
{
  "tag": "cloudflare",
  "type": "dot",
  "settings": { "server": "1.1.1.1", "timeout": "5s" },
  "tls": { "server_name": "one.one.one.one" },
  "outbound": "direct-fragment",
  "ip_versions": "ipv4_only",
  "dns_server": "os"
}
```

</div>

<div class="cfg-fields" markdown>

### `tag`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

The name rules, `default_server`, `resolve` and `dns_server` use. Unique among
DNS servers.

### `type`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

| Type | Protocol | `server` port default | TLS |
| --- | --- | --- | --- |
| `udp` | DNS over UDP (RFC 1035) | `53` | no |
| `tcp` | DNS over TCP (RFC 7766) | `53` | no |
| `dot` | DNS over TLS (RFC 7858) | `853` | yes |
| `doh` | DNS over HTTPS (RFC 8484), HTTP/1.1 | `443` | yes |
| `system` | the operating system's resolver | none | no |

`apranik modules` lists the types this build has. `system` is built by core:
it takes no `outbound`, no `dns_server`, and only `timeout` in its settings.

### `settings`

<span class="cfg cfg-type">object</span>

The type's own settings:

- **`server`** (`udp`, `tcp`, `dot`, `doh`; required): where the DNS server
  is, an IP or a hostname, with an optional port: `"8.8.8.8"`,
  `"dns.google:853"`, `"[2001:4860:4860::8888]"`. A hostname is resolved with
  [`dns_server`](#dns_server) first. A `udp` server should be an IP.
- **`timeout`** (all types; default `5s`): how long one query may take. For
  `tcp`, `dot` and `doh` it includes reaching the server; the outbound's own
  layers keep their own timeouts too. Must be greater than zero.
- **`path`** (`doh`; default `/dns-query`): the request path.
- **`host`** (`doh`; default: the server's host): the `Host` header. Set it
  when `server` is an IP: `8.8.8.8` serves `dns.google`.

`tcp` and `dot` keep one connection open and send queries on it without
waiting for earlier answers; `doh` keeps its connections alive. `udp` sends
each query from a fresh port and takes only an answer from the server's
address with the query's ID, ignoring anything else, such as a forged
packet that races the real answer. A `udp` answer too large for one datagram
is an error that says to use `tcp` or `dot` for that server.

### `tls`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-def">Default: the <code>tls</code> transport's defaults</span>

The [`tls` transport's settings](../transport/tls.md) for the TLS between
Apranik and the DNS server. Only `dot` and `doh` use TLS; `check` refuses
`tls` on the others. Without the block, the server name is the server's
hostname (for an IP, none is sent and the certificate must carry the IP), and
the fingerprint is Chrome's.

`doh` speaks HTTP/1.1 only, so core gives its `tls` `"alpn": ["http/1.1"]`
when you write no `alpn`. Writing one that offers anything else (`h2`) is
refused by `check`: a server choosing it would fail every query.

This TLS runs end to end with the DNS server. Through a proxy outbound there
are two: the outbound's, to the proxy server, and this one, inside the tunnel
to the DNS server; the censor sees only the outbound's.

### `outbound`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: a plain freedom</span>

The outbound the server's queries go through: any outbound, such as a freedom
with [`fragment`](../transport/fragment.md), so the DNS server's TLS
ClientHello is cut, or the proxy, so the network sees no DNS at all. Path
techniques are configured once, on outbounds, and every one of them works for
DNS. A DNS server has no transport of its own.

Without it, core uses an internal freedom, tagged `dns-default-freedom`,
which no user outbound may use. `check` warns when such a server sits beside
a proxy default route: its queries go out directly and show the network
which names are looked up. A `udp` server needs an outbound that carries UDP.

### `ip_versions`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: the section's <code>ip_versions</code></span>

[`ip_versions`](#ip_versions) for lookups sent to this server: for one that
answers IPv6 badly, `ipv4_only`.

### `dns_server`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: <code>default_server</code></span>

The DNS server that resolves this server's own hostname, when `server` is
one. The same field on a proxy outbound resolves that proxy's `server`.

A lookup that would wait on itself is refused. `check` reports the loops it
can see, naming each server and outbound on them:

```
dns.servers[0]: these lookups wait on each other: dns server "google" →
outbound "proxy" → dns server "google"; give one of them a dns_server that
does not go through the others
```

Some loops only a lookup reveals: a hostname inside a server's `settings`, or
a name the rules send somewhere unexpected. Those fail at the first lookup
with the same kind of message, never hang.

</div>

## Rules

<div class="cfg-structure" markdown>

```json
{ "match": { "domain_suffix": ["ir"] }, "server": "domestic" }
```

</div>

A rule is a `match` and the `server` that answers the names it matches. The
match is a [routing match](../routing/index.md#match) restricted to the name,
which is all a lookup has: `domain`, `domain_suffix`, `domain_keyword`,
`domain_regex`, `domain_in_dataset`, and `any`, `all` and `not` over them.
Datasets are the config's top-level [`datasets`](../routing/datasets.md).
Rules are separate from routing rules: those choose an outbound for traffic,
these a server for a name.

## Examples

Iranian names from a domestic resolver, everything else over DoH through the
proxy, and the proxy's own name from the domestic resolver, so it never loops:

```json
"outbounds": [
  { "tag": "proxy", "server": "example.com", "server_port": 443, "dns_server": "domestic", "...": "..." },
  { "tag": "direct", "protocol": { "type": "freedom" } }
],
"dns": {
  "servers": [
    { "tag": "google", "type": "doh", "settings": { "server": "8.8.8.8", "host": "dns.google" },
      "tls": { "server_name": "dns.google" }, "outbound": "proxy" },
    { "tag": "domestic", "type": "udp", "settings": { "server": "192.0.2.53" }, "outbound": "direct" }
  ],
  "rules": [{ "match": { "domain_in_dataset": ["ir-domains"] }, "server": "domestic" }],
  "default_server": "google"
}
```

DoT straight to Cloudflare with its ClientHello fragmented:

```json
"outbounds": [
  { "tag": "direct-fragment",
    "transport": [{ "type": "tcp" }, { "type": "fragment" }],
    "protocol": { "type": "freedom" } }
],
"dns": {
  "servers": [
    { "tag": "cloudflare", "type": "dot", "settings": { "server": "1.1.1.1" },
      "tls": { "server_name": "one.one.one.one" }, "outbound": "direct-fragment" }
  ],
  "default_server": "cloudflare"
}
```

## What `check` reports

Errors:

- no servers, no `default_server`, or a tag that does not exist (server,
  outbound, dataset, `dns_server`, a rule's `server`, a `resolve`'s `server`);
- a server tag used twice, an unknown `type`, a type's settings it refuses;
- `tls` on a type that does not use TLS, or an ALPN offer the type cannot
  speak;
- a `udp` server on an outbound that cannot carry UDP;
- `outbound` or `dns_server` on `system`;
- a rule field other than the name fields, or a rule after one that matches
  every name;
- `serve_stale` with the cache disabled, a `max_entries` or `max_stale_age` that is zero or negative, an unknown
  `ip_versions`;
- a loop of lookups that wait on each other;
- `dns_server` on freedom, which has no server name of its own;
- an outbound tagged `dns-default-freedom`.

Warnings:

- a server with no `outbound` while the default route is a proxy;
- a `udp` server under a freedom whose transport only shapes TCP;
- a `udp` server given as a hostname.

## Not yet

Intercepting the DNS queries clients send (`intercept_dns`, which needs UDP
sniffing), fake IPs, DNS over QUIC and HTTP/3, filtering poisoned answers by
expected address ranges, matching on query type, `ip_versions` per rule, a
TTL clamp, and retrying a truncated `udp` answer over TCP automatically.
