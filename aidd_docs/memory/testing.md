# Testing

## Strategy

- Unit: engine and tools against the in-process fake (`internal/logto/logtofake`), no network.
- Integration: tests tagged `integration` against a throwaway real Logto (`test/integration/compose.yaml`), one
  version of the support window at a time; credentials through the `m-default` route (`build/it.env`, 0600).
- End-to-end: Chainsaw on a throwaway k3d cluster (`test/e2e`); today only a smoke test of the harness.

## Tools

- `go test` with the race detector.
- The fake is built on the generated chi server: routing and parameter parsing come from the spec.
- Chainsaw for declarative Kubernetes assertions, k3d for the cluster.

## Conventions

- Tests first; a guard is shown failing once before it is trusted.
- The fake overrides only what a test needs; every other route answers 501, so a test cannot pass against an
  unmodelled route.
- Never a deployed Logto in a test: the fake, a local Logto, or a Logto started inside the CI job.
- Test names state the behaviour (`TestARouteTheFakeDoesNotModelAnswers501`).
- Every kind has three tests: `contract_test.go` (its wire types against every vendored spec), `<kind>_test.go`
  (the fake: create, replay without writes, adoption, refusals, prune) and `integration_test.go` (a real Logto,
  objects named with `kindtest.Unique`, prune limited by `kindtest.KeepAllBut`).
- `test/integration/acceptance_test.go` is the M1 definition of done: the built binary on `examples/reference.yaml`.
- Under `task it` (`LOGTO_IT_REQUIRED`), a missing harness fails instead of skipping: `go test` prints `ok` for a
  skipped test too.

## Run

- `task test` for unit tests.
- `task it` (newest version, or `LOGTO_VERSION=`), `task it:all` (every version), `task it:down` cleans up.
- `task e2e` for the e2e suite (creates the cluster if missing, its own kubeconfig in `build/`); `task e2e:cluster:down` removes it.
