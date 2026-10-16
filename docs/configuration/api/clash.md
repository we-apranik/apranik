# Clash API

Lets a dashboard (MetaCubeXD, yacd, zashboard) or an app read and control a running
Apranik through the API that [Mihomo](https://github.com/MetaCubeX/mihomo) defined.
The endpoints are on [Clash API endpoints](clash-endpoints.md).

## Structure

<div class="cfg-structure">

```json
{
  "type": "clash",
  "settings": {
    "listen": "127.0.0.1",
    "port": 9090,
    "secret": "a long random string",
    "allow_origins": ["https://metacubex.github.io"],
    "external_ui": "/opt/apranik/ui",
    "log_buffer_records": 256,
    "delay_test": {
      "url": "https://www.gstatic.com/generate_204",
      "timeout": "10s",
      "expected_status": "200-299"
    }
  }
}
```

</div>

<div class="cfg-fields">

## Fields

### `listen`

<span class="cfg cfg-type">IP address</span>
<span class="cfg cfg-req">Required</span>

The address to listen on. Off loopback, `check` warns. On a phone, loopback is
reachable by every app, so keep the secret long.

### `port`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-req">Required</span>

The port to listen on.

### `secret`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-req">Required</span>

Every request sends it as `Authorization: Bearer <secret>`. A WebSocket handshake,
which a browser cannot give headers, may send `?token=<secret>` instead.

### `allow_origins`

<span class="cfg cfg-type">list of origins</span>
<span class="cfg cfg-def">Default: none</span>

The web pages allowed to call the API, as `scheme://host[:port]`. `"*"` allows every
page, with a warning. Programs that are not browsers are not affected.

### `external_ui`

<span class="cfg cfg-type">string</span>
<span class="cfg cfg-def">Default: none</span>

An absolute path to an unpacked dashboard, served at `/ui/` without the secret.

### `log_buffer_records`

<span class="cfg cfg-type">integer</span>
<span class="cfg cfg-def">Default: <code>256</code></span>

How far one log stream may fall behind before it loses records, up to 100,000.

### `delay_test`

<span class="cfg cfg-type">object</span>

Defaults for the delay endpoints: `url`, `timeout` and `expected_status`, as in a
[probe](../shared/probe.md).

</div>
