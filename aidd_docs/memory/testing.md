# Testing

## Strategy

- Unit: tools and engine tested in-process, no network.

## Tools

- `go test` with the race detector.

## Conventions

- Tests first; a guard is shown failing once before it is trusted.
- Never a deployed Logto in a test.
- A test against the vendored spec guards the generator workarounds: an upgrade that moves an overlay target fails
  there (`tools/specnorm`).
- Test names state the behaviour (`TestAnOverlayWhoseTargetIsGoneFails`).

## Run

- `task test`.
