# Routing rule

One entry of `routing.rules`: a `match` and an [`action`](action.md).

## Structure

<div class="cfg-structure">

```json
{
  "match": {
    "network": "tcp",
    "any": [
      { "domain_suffix": ["ir"] },
      { "ip_in_dataset": ["ir-ips"] }
    ],
    "not": { "port": [22] }
  },
  "action": {
    "type": "route",
    "settings": { "outbound": "direct" }
  }
}
```

</div>

## Match

Different fields must all hold; the values of one field are alternatives. An empty
`match` matches every request. The example above matches TCP to an Iranian name or
address, except to port 22.

### Logic

| Field | Format | Holds when |
| --- | --- | --- |
| `any` | list of matches | at least one holds |
| `all` | list of matches | every one holds |
| `not` | match | it does not hold |

### Request

| Field | Format | Compares |
| --- | --- | --- |
| `inbound` | list of tags | the inbound the request arrived on |
| `network` | `"tcp"` or `"udp"` | the network |
| `user` | list of names | the [user](../inbound/users.md) who logged in |
| `sniffed_protocol` | list: `tls`, `http` | what a [`sniff`](action.md#sniff) recognised |
| `sniffed_request_header` | object: header → regex | headers of a sniffed HTTP request |
| `sniffed_browser_family` | list: `chromium`, `firefox`, `safari` | the browser a sniffed ClientHello looks like |

`sniffed_request_header` holds when every header it names matches its Go regular
expression, such as `{ "user-agent": "^curl/" }`. Names are lowercase; `:method` and
`:path` are included. `""` only requires the header to exist.

`sniffed_browser_family` is a guess, empty when the sniffer is unsure.

### Domain

Domain fields read the sniffed name if there is one, else the destination's domain.
The destination itself is never changed.

| Field | Format | Matches |
| --- | --- | --- |
| `domain` | list | exactly this name |
| `domain_suffix` | list | this name and every name under it |
| `domain_keyword` | list | names containing it |
| `domain_regex` | list | names a Go regular expression matches |
| `domain_in_dataset` | list of tags | names in a [domain dataset](../datasets.md) |

### Address and port

IP fields read a destination that is an IP, or the addresses a
[`resolve`](action.md#resolve) found; the router never resolves by itself.

| Field | Format | Compares |
| --- | --- | --- |
| `ip` | list of IPs or CIDRs | the destination IP |
| `ip_in_dataset` | list of tags | the destination IP, in an [IP dataset](../datasets.md) |
| `ip_is_private` | boolean | whether the destination IP is private or reserved |
| `ip_version` | list: `4`, `6` | the destination IP's version |
| `port` | list of numbers | the destination port |
| `port_range` | list of `"first-last"` | the destination port |
| `source_ip` | list of IPs or CIDRs | the client's IP |
| `source_ip_in_dataset` | list of tags | the client's IP, in an IP dataset |
| `source_ip_is_private` | boolean | whether the client's IP is private or reserved |
| `source_port` | list of numbers | the client's port |
| `source_port_range` | list of `"first-last"` | the client's port |
| `inbound_local_ip` | list of IPs or CIDRs | the address of this machine the client reached |
| `inbound_local_port` | list of numbers | the port the client reached |
| `inbound_local_port_range` | list of `"first-last"` | the port the client reached |

"Private or reserved" covers private, loopback, link-local, multicast and unspecified
addresses, and the reserved IPv4 ranges such as `100.64.0.0/10` and `198.18.0.0/15`.
