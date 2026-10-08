# APIs

An API is a way for another program, a client app or a server panel, to read and drive
a running Apranik: its traffic, its live conversations, its outbounds, its log, a manual
choice of server. An API is a [module](../architecture.md) like a proxy protocol or a DNS
server: it is named by `type`, takes its own `settings`, and is registered in
`core/registry/default.go`. Which APIs a build has is in [`apranik modules`](../cli.md).

This build has one API, the [Clash API](api/clash.md). This page is about the section every
API shares.

<div class="cfg-structure" markdown>

```json
{
  "apis": [
    { "type": "clash", "settings": { "listen": "127.0.0.1", "port": 9090, "secret": "..." } }
  ]
}
```

</div>

## `apis`

<span class="cfg cfg-type">list of objects</span>
<span class="cfg cfg-def">Default: none; no API runs</span>

Each entry is `{"type", "settings"}`. Two entries of one type are allowed, for two ports.
What `settings` holds, address, port and credentials included, is the module's: see its
page. `check` reports a setting at `apis[0].settings.<field>`.

## When an API starts and stops

`apranik run` builds everything, activates the inbounds, starts probing, and **then**
starts each API, in the order of the list. If one cannot start, for example its port is
taken, `run` stops what it started and ends with that error: you never get a core that
is half up.

On shutdown the APIs close **first**, so an open request, a log stream for instance, ends
before anything it reads goes away.

`apranik check` never starts an API and opens nothing. It runs each module's validation
and warnings only, so **a taken port shows when you run, not when you check.**

## What an API can see

An API never imports core. Core gives it one provider with small, separate questions:
the totals, the live conversations, closing one, the log, the outbounds and their state,
a delay test, choosing a selector's member, a DNS lookup and flushing the DNS cache, the
routing rules, and loading the config again. An API that asks for something that is
switched off gets a plain answer saying so: statistics disabled, live conversations not
tracked.

An API that can see live conversations and the log can see where users go. Give it a strong
secret and a loopback address, and read [Stats](stats.md) and [Log](log.md).
