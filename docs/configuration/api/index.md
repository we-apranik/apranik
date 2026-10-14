# API

A way for another program, a client app or a server panel, to read and control a
running Apranik. Each API is a module with its own listener. This build has one:
the [Clash API](clash.md).

## Structure

<div class="cfg-structure" markdown>

```json
{
  "apis": [
    {
      "type": "clash",
      "settings": {
        "listen": "127.0.0.1",
        "port": 9090,
        "secret": "a long random string"
      }
    }
  ]
}
```

</div>

<div class="cfg-fields" markdown>

## Fields

### `type`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

The API module: `clash`.

### `settings`

<span class="cfg cfg-type">object</span>

The module's settings, address and secret included.

</div>

## Lifecycle

- `apranik run` starts the APIs last, after the inbounds are up, and closes them
  first. If one cannot start, for example its port is taken, `run` stops.
- `apranik check` never starts an API, so a taken port shows only at `run`.
- An API can see where users go and can change users. Give it a long secret and a
  loopback address.
