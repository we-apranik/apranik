## What and why

<!-- One change with one purpose. Link the issue. -->

## Checklist

- [ ] `gofmt -l .` prints nothing; `go vet ./...` and `go test -race ./...` pass
- [ ] A new module has tests that run without core
- [ ] Docs updated, and `npm run build` in `docs/` passes if they changed
- [ ] No new dependency, or its justification is below
- [ ] I did not touch `spec/`, the builder or another module, or I say why here
