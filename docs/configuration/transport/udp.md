# UDP

The base datagram transport. Today it is configured only inside
[`freedom`](../outbound/freedom.md#udp)'s settings, and it has no settings of
its own.

A datagram addressed to a domain is resolved through the
[resolver](../dns/index.md), like every other lookup: each DNS server bounds
its own queries with its `timeout`, and answers are cached for their TTL under
[`dns.cache`](../dns/index.md#cache). `udp` has no settings for either.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "udp": {}
}
```

</div>
