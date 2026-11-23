# Coding Assertions

## Before commit

| Order | Command | Checks |
| ----- | ------- | ------ |
| 1 | `task lint` | golangci-lint (generated files excluded), actionlint + shellcheck, `goreleaser check`, `ci:runners` guard |
| 2 | `task test` | unit tests with the race detector |

## Before push

| Order | Command | Checks |
| ----- | ------- | ------ |
| 1 | `task check` | generate drift, `go mod tidy`, lint, unit tests, envtest, build, image build (= `ci.yml`) |
| 2 | `task e2e` | Chainsaw suite on a throwaway k3d cluster, needs Docker (= `e2e.yml`) |

`task generate:check` compares against the git index: it fails on generated files that are not yet committed.

## Behavior

If a fix is needed, spawn 1 agent per assertion to fix (e.g typechecking / tests / rules violated on category UI = 3 agents).
