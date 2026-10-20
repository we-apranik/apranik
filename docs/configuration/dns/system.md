# System DNS

The `system` server asks the operating system's resolver. It is the implicit server when there is no `dns` section. The system reports no TTL, so its answers are kept for 30 seconds.

## Structure

<div class="cfg-structure">

```json
{
  "tag": "os",
  "type": "system",
  "settings": { "timeout": "5s" }
}
```

</div>

<div class="cfg-fields">

## Fields

### `settings.timeout`

<span class="cfg cfg-type">duration</span>
<span class="cfg cfg-def">Default: <code>5s</code></span>

How long one query may take, including reaching the server.

</div>

It takes only `tag`, `type` and `settings.timeout`: no `outbound`, `tls`, `ip_versions` or `dns_server`. The query goes where the operating system sends it, outside Apranik's outbounds.

`tag` is described on [DNS server](server.md).
