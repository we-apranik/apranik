# Routing

Decides where each request goes: to an outbound, or nowhere. Rules are checked in
order; the first rule whose action decides wins, and `default_action` takes the
rest. Without a `routing` section, every request takes the first outbound.

[![How routing rules are evaluated: each rule in order is checked; if its match holds its action runs. route sends the request through an outbound and block refuses it, both ending the list. sniff learns the name the connection asks for and resolve looks up the domain's addresses; both go on to the next rule. A request no rule decided takes default_action.](../../assets/diagrams/routing-evaluation.excalidraw.svg)](../../assets/diagrams/routing-evaluation.excalidraw.svg)

## Structure

<div class="cfg-structure">

```json
{
  "routing": {
    "rules": [
      {
        "match": { "domain_suffix": ["ir"] },
        "action": {
          "type": "route",
          "settings": { "outbound": "direct" }
        }
      }
    ],
    "default_action": {
      "type": "route",
      "settings": { "outbound": "proxy" }
    }
  }
}
```

</div>

<div class="cfg-fields">

## Fields

### `rules`

<span class="cfg cfg-type">list of rules</span>

Each rule is a [`match`](rule.md) and an [`action`](action.md).

### `default_action`

<span class="cfg cfg-type">action</span>
<span class="cfg cfg-req">Required</span>

What happens to a request no rule decided: [`route`](action.md#route) or
[`block`](action.md#block).

</div>

## Example

A client in Iran: ads blocked, local and Iranian destinations direct, QUIC blocked
so browsers fall back to TCP, everything else through the proxy.

```json
{
  "datasets": [
    {
      "tag": "ads",
      "source": { "file": "geosite.dat" },
      "format": {
        "type": "geodata",
        "kind": "domain",
        "category": "category-ads-all"
      }
    },
    {
      "tag": "ir-domains",
      "source": { "file": "geosite.dat" },
      "format": {
        "type": "geodata",
        "kind": "domain",
        "category": "category-ir"
      }
    },
    {
      "tag": "ir-ips",
      "source": { "file": "geoip.dat" },
      "format": { "type": "geodata", "kind": "ip", "category": "ir" }
    }
  ],
  "routing": {
    "rules": [
      {
        "match": { "network": "tcp" },
        "action": {
          "type": "sniff",
          "settings": {
            "sniffers": [{ "type": "tls" }, { "type": "http" }]
          }
        }
      },
      {
        "match": { "domain_in_dataset": ["ads"] },
        "action": { "type": "block" }
      },
      {
        "match": {
          "any": [
            { "ip_is_private": true },
            { "domain_in_dataset": ["ir-domains"] },
            { "ip_in_dataset": ["ir-ips"] }
          ]
        },
        "action": {
          "type": "route",
          "settings": { "outbound": "direct" }
        }
      },
      {
        "match": { "network": "udp", "port": [443] },
        "action": { "type": "block" }
      }
    ],
    "default_action": {
      "type": "route",
      "settings": { "outbound": "proxy" }
    }
  }
}
```
