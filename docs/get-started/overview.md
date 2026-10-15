# What is Apranik

Apranik is a **proxy core**: the part of a circumvention app that accepts traffic
from the device, decides where it goes, and carries it there over whatever
protocol and transport still gets through.

Client apps (Hiddify, v2rayNG, NekoBox, v2box) and server panels run the `apranik`
binary with a config file, as they run Xray or sing-box. They provide the user
interface; Apranik is the engine. Its Go packages can be used as a library too.

## The problem

In a heavily filtered network, the techniques that work change faster than proxy
cores release. Iran is the motivating case, though nothing in the code is specific
to it. What limits a response is how quickly a new technique can be added,
reviewed and shipped.

## The approach

**A new technique is one module.**

- Every contract lives in one package, `spec`, which imports nothing else from
  the project.
- A module is a folder under `module/` that implements a `spec` interface.
- One line in `core/registry/default.go` makes it available to config files.
- Its tests run without the rest of the core.

[![From a technique to a deployed module: a researcher finds a technique, writes one folder under module/, tests it there without core, adds one line to core/registry/default.go, names it by type in a config file that apranik check validates, and it ships in the apranik binary that clients and panels run. spec, the builder, core's inbound and outbound, other modules and the config parser stay untouched.](../assets/diagrams/research-to-deployment.excalidraw.svg)](../assets/diagrams/research-to-deployment.excalidraw.svg)

If a technique needs more, such as an edit to `spec` or to another module, the
contract is missing a general concept, and that concept is added once for every
later module.

## What it is

- **A core.** One `apranik` binary that apps and panels drive with a config file.
- **A framework for new techniques.** What it offers is how quickly a technique
  becomes a module, not how many protocols it lists.
- **Independent.** Its code is its own, written to be adapted quickly, alongside
  mature cores such as Xray, sing-box and mihomo.

## Dependencies

The standard library wherever it can, plus two: uTLS, to send a browser's TLS
ClientHello, and `golang.org/x/net`, for DNS messages and the Clash API's WebSocket.

## Next

- [Install and run](install.md) and write a [first configuration](first-configuration.md).
- [Architecture](../concepts/architecture.md): how the pieces fit.
