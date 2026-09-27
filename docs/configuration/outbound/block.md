# Block

`block` refuses every connection and drops every UDP packet. It is a special
outbound protocol, built by core.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "tag": "drop",
  "protocol": { "type": "block" }
}
```

</div>

It takes no settings. Of the [outbound fields](index.md), only
[`tag`](index.md#tag) applies: block dials nothing, so `dial_timeout` is refused
too.
