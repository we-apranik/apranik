# DNS rule

Which [DNS server](server.md) answers which names, in `dns.rules`. Rules are
checked in order and the first match wins; `default_server` answers the rest.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "match": { "domain_suffix": ["ir"] },
  "server": "domestic"
}
```

</div>

<div class="cfg-fields" markdown>

## Fields

### `match`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-req">Required</span>

A [routing match](../routing/rule.md#match) limited to the name: `domain`,
`domain_suffix`, `domain_keyword`, `domain_regex`, `domain_in_dataset`, and `any`,
`all` and `not` over them.

### `server`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

The tag of the DNS server that answers.

</div>
