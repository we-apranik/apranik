# Datasets

Named lists of domains or IP ranges, read from files, that
[routing rules](routing/rule.md) and [DNS rules](dns/rule.md) look up by tag. A
dataset holds values only; rules decide what to do with them.

## Structure

<div class="cfg-structure">

```json
{
  "datasets": [
    {
      "tag": "ir-domains",
      "source": { "file": "geosite.dat" },
      "format": {
        "type": "geodata",
        "kind": "domain",
        "category": "category-ir"
      }
    }
  ]
}
```

</div>

<div class="cfg-fields">

## Fields

### `tag`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

A name unique among datasets. Rules use a domain dataset in `domain_in_dataset`, an
IP dataset in `ip_in_dataset` and `source_ip_in_dataset`.

### `source.file`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

A path, relative to the config file (or to the `apranik` executable, for a config
loaded from a URL). Downloading is not built yet.

### `format.type`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

Only `geodata`: V2Ray's `geosite.dat` and `geoip.dat`, as Xray reads them.

### `format.kind`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

`domain` or `ip`, checked against the file.

### `format.category`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

The list inside the file, compared without case: `category-ir`, `category-ads-all`,
`ir`, `private`. Names depend on who built the file.

### `format.attribute`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-note">domain only</span>

Keep only entries with this attribute, such as `cn` in the `google` category.

</div>

A geosite entry keeps its own kind of match: `domain` matches like
`domain_suffix`, `full` like `domain`, `plain` like `domain_keyword`, `regexp` like
`domain_regex`. Files are read at `check` and again at start; Apranik never
downloads or updates them.
