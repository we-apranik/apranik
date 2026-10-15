# Security

Apranik is meant for people who may be harmed if it fails. Please report
vulnerabilities privately, so a fix can ship before the details are public.

!!! warning "Not audited"
    Apranik has not had an independent security review, and its configuration
    format is not stable yet. Do not rely on it for your safety until it has.

## Report a vulnerability

Use either channel. Do not open a public issue, pull request or discussion for it.

1. **GitHub private reporting.** Open
   [a private security advisory](https://github.com/we-apranik/apranik/security/advisories/new).
   Only the maintainers can see it.
2. **Email**, for people without a GitHub account: we-apranik@proton.me.

[![How a vulnerability report is handled: the reporter uses a GitHub private advisory or email, never a public issue. The maintainers acknowledge within about a week, prepare a fix in private and agree a date with the reporter, then release the fix, publish the advisory and credit the reporter. Reporters publish after up to 90 days.](../assets/diagrams/vulnerability-report.excalidraw.svg)](../assets/diagrams/vulnerability-report.excalidraw.svg)

Please include:

- what you found and what an attacker gains (a crash, a bypass, a leak, remote code);
- the version or commit, and the configuration that shows it (remove real
  addresses and credentials);
- steps or a small program that reproduces it;
- whether you plan to publish, and when.

## What counts

Reports about the `apranik` binary and its Go packages are welcome, in particular:

- a remote peer that crashes, hangs or exhausts memory in a listener, a parser
  or a handshake;
- authentication or authorization bypass, including users and the APIs;
- a way to make traffic leave outside the configured route, or a DNS or
  address leak;
- secrets or the destinations users reached appearing in logs, errors or the API;
- a TLS, transport or fingerprinting weakness that lets a censor tell Apranik
  traffic from ordinary traffic.

Out of scope: vulnerabilities in a client app or panel that runs Apranik, in Go
or uTLS themselves (report those upstream), findings that need a user to
already control the machine, and denial of service by sheer traffic volume.

## What to expect

Apranik is a small project. We aim to acknowledge a report within a week, to
agree a fix and a disclosure date with you, and to credit you if you wish. We
ask for up to 90 days before you publish, and will tell you if we need more.

Test only against systems you own. Do not access other people's data or
disrupt a service.

## Supported versions

There is no release yet. Fixes go to the `main` branch.
