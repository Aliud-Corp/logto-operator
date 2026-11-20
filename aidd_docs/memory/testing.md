# Testing

## Strategy

- Unit: engine and tools against the in-process fake (`internal/logto/logtofake`), no network.

## Tools

- `go test` with the race detector.
- The fake is built on the generated chi server: routing and parameter parsing come from the spec.

## Conventions

- Tests first; a guard is shown failing once before it is trusted.
- The fake overrides only what a test needs; every other route answers 501, so a test cannot pass against an
  unmodelled route.
- Never a deployed Logto in a test: the fake, a local Logto, or a Logto started inside the CI job.
- A test against the vendored spec guards the generator workarounds: an upgrade that moves an overlay target fails
  there (`tools/specnorm`).
- Test names state the behaviour (`TestARouteTheFakeDoesNotModelAnswers501`).

## Run

- `task test`.
