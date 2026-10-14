# Routing action

What a rule does when its match holds, and what `default_action` does. `route` and
`block` decide; `sniff` and `resolve` add facts and go on to the next rule.

## `route`

```json
{
  "type": "route",
  "settings": { "outbound": "direct" }
}
```

Sends the request through the outbound with this tag.

## `block`

```json
{
  "type": "block"
}
```

Refuses the request: a TCP connection is closed, UDP datagrams are dropped.

## `sniff`

```json
{
  "type": "sniff",
  "settings": {
    "sniffers": [{ "type": "tls" }, { "type": "http" }],
    "timeout": "300ms"
  }
}
```

Reads the start of a TCP connection to learn the name it asks for, then goes on.
See [Sniffing](sniffing.md). Put rules that need no name, such as SSH by port, before
it: a client that waits for the server to speak first waits `timeout`.

## `resolve`

```json
{
  "type": "resolve",
  "settings": { "server": "cloudflare" }
}
```

Looks the destination's domain up, so the rules after it can match its addresses
with IP fields. `server` is optional; without it the [DNS rules](../dns/rule.md)
choose. The destination is not rewritten, and a failed lookup leaves no addresses.

`default_action` may be `route` or `block` only.
