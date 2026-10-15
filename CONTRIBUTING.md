# Contributing to Apranik

Thank you for helping. The full guide is on the docs site:
**[we-apranik.github.io/apranik/contribute](https://we-apranik.github.io/apranik/contribute/)**.
It starts with [your first module](https://we-apranik.github.io/apranik/contribute/first-module/),
a sniffer from an empty folder to a working rule.

## The short version

- You need Go (the version in `go.mod`) and nothing else.
- Before a pull request, these must be clean:

  ```sh
  gofmt -l .        # prints nothing
  go vet ./...
  go test -race ./...
  ```

- A new technique is one folder under `module/` and one line in
  `core/registry/default.go`, with tests that run without core. If you had to edit
  `spec/`, the builder or another module, say so in the description: the contract
  is missing something general, and that is worth an issue first.
- Names say what things do; a comment is for a reason the code cannot show.
- A new dependency needs a written justification.

## Before you start

Open an issue for anything larger than a fix, and always before a change to
`spec/`. A change to the contract is a design decision.

## Security

Do not open a public issue for a vulnerability. See [SECURITY.md](SECURITY.md).

## Conduct

Everyone taking part follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## Licence

By contributing you agree that your work is released under the
[Mozilla Public License 2.0](LICENSE).
