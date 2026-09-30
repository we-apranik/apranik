# Datasets

A dataset is a named list of domains or of IP ranges, read from a file, that
[routing rules](index.md) look up: "every Iranian domain", "every Iranian IP
range", "ad servers". A dataset holds values only. What to do with them is
written in the rules.

## Structure

<div class="cfg-structure" markdown>

```json
{
  "routing": {
    "datasets": [
      {
        "tag": "ir-domains",
        "source": { "file": "geosite.dat" },
        "format": { "type": "geodata", "kind": "domain", "category": "category-ir" }
      }
    ]
  }
}
```

</div>

Rules use a dataset by its tag, in the field for its kind:
`domain_in_dataset` for a domain dataset, `ip_in_dataset` and
`source_ip_in_dataset` for an IP dataset.

<div class="cfg-fields" markdown>

## Fields

### `tag`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

The name rules use. Unique among datasets.

### `source`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-req">Required</span>

Where the file is. Only `file` exists:

- **`file`**: a path. A relative path is relative to the config file's
  directory, or, for a config loaded from a URL, to the directory of the
  `apranik` executable.

Downloading a dataset from a URL is not built yet.

### `format`

<span class="cfg cfg-type">object</span>
<span class="cfg cfg-req">Required</span>

How to read the file:

- **`type`**: the file format. Only `geodata` exists.
- **`kind`**: `domain` or `ip`. Required for every format, and checked against
  the file.
- **`category`**, for `geodata`: the list inside the file, compared without
  regard to case: `category-ir`, `category-ads-all`, `ir`, `private`.
- **`attribute`**, for `geodata` domain lists, optional: keep only the entries
  carrying this attribute, such as `cn` in the `google` category.

</div>

## `geodata`

V2Ray's `geosite.dat` and `geoip.dat`, which Xray also reads and which
clients and panels already ship. One file holds many categories; Apranik reads
only the ones a config names. A geosite entry keeps its own kind of match:

| Entry in the file | Matches like |
| --- | --- |
| domain | [`domain_suffix`](index.md#domains) |
| full | [`domain`](index.md#domains) |
| plain | [`domain_keyword`](index.md#domains) |
| regexp | [`domain_regex`](index.md#domains) |

Category names depend on who built the file: v2fly's list names Iran's
`category-ir`, while Iran-focused files often call it `ir`. `apranik check`
reports a category the file does not have.

The files are read when the config is checked and again when it runs; nothing
is downloaded or updated by Apranik.
