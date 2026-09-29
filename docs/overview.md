# Overview

## What Apranik is

Apranik is a **proxy core**: the part of a circumvention client that accepts
traffic from the device, decides where it should go, and carries it there over
whatever protocol and transport currently gets through. It is written in Go
and uses only the standard library.

The main product is the `apranik` binary (`cmd/apranik`). Client apps such as
Hiddify, v2rayNG, NekoBox and v2box, and server panels, run it with a config file,
the way they run Xray-core or sing-box. Apranik is the engine; they provide the
user interface, subscription handling and platform integration. The same
functionality is available as Go packages for programs that want to link it in.

## What Apranik is not

- **Not a GUI or client application.** There is no user interface, and one is not
  planned.
- **Not a protocol collection.** Protocol count is not the goal. Today it ships three
  proxy protocols (SOCKS5, HTTP and Trojan), two base transports (TCP and UDP) and
  three overlays (TLS, WebSocket and HTTPUpgrade), because those are enough to
  prove the architecture and to carry real traffic through a censor.
- **Not a replacement narrative.** Xray-core, sing-box and mihomo are mature and
  useful. The claim is narrower: their architecture is not optimised for rapid,
  Iran-specific adaptation.
- **Not a fork.** Apranik shares no code with those projects.

## The problem

Iran is the motivating environment, though nothing in the code is specific to it.
In a heavily filtered network, the techniques that work change faster than proxy
cores release. What limits a response is how quickly a new technique can be
added, reviewed and shipped to users.

In many cores, adding a transport or protocol means touching shared dispatch code,
a central configuration schema and other modules. Each touch is a review burden and
a chance to break something unrelated.

## How Apranik approaches it

Everything is organised around one property: **a new technique is one module.**

- All contracts live in one package, [`spec`](architecture.md#the-spec-contract),
  which imports nothing from the project.
- A module is a folder under `module/` that implements one or more `spec`
  interfaces and exports a constructor for each.
- It becomes available to config files through one line in
  `core/registry/default.go`.
- Its tests run without the rest of the core, because the contract is all it
  depends on.

[![From a technique to a deployed module: a researcher finds a technique, writes one folder under module/ implementing one spec kind, tests it without core, adds one line to core/registry/default.go, names it by "type" in a config file that apranik check validates, and ships it in a release of the apranik binary that clients and panels pick up (planned).](assets/diagrams/research-to-deployment.excalidraw.svg)](assets/diagrams/research-to-deployment.excalidraw.svg)


If adding a technique needs more than that (an edit to `spec`, the builder or
another module), the contract is missing something, and the project records and
fixes it rather than accepting the cost. Fixing it means giving the contract a
new, general concept, one that at least two modules need, so the next technique
of that kind needs no edit. Code in one module that knows another module by name
is never the fix.

## Measured so far

The claim is tested by adding modules and counting the files each one needed
outside its own folder.

| Module | Outside its folder | Verdict |
| --- | --- | --- |
| `tls` (first overlay) | one registry line, plus a one-time change to the overlay contract and core | the change was a design finding: overlays wrapping listeners let one silent client stall an inbound |
| `ws` | one registry line | as intended |
| `httpupgrade` | one registry line, plus moving the handshake it shares with `ws` to `shared/websocket/handshake` | as intended: shared code moves out once a second user exists |

VLESS, the next protocol, is the next measurement.

## Where to go next

- [Architecture](architecture.md) for how the pieces fit.
- [Request lifecycle](request-lifecycle.md) to follow real traffic through the code.
