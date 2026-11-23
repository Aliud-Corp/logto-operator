# Deployment

## Pipeline

- GitHub Actions on GitHub-hosted runners only (`ubuntu-<version>`, pinned): the repository is public, and a
  fork's pull request must never run on self-hosted infrastructure. `task ci:runners` fails on any other `runs-on`.
- `ci.yml`: one `check` job running `task check`. `e2e.yml`: `task e2e`, path-filtered on what it exercises.
- `integration.yml`: one job, path-filtered; pushes to `main`, and pull requests that touch `internal/engine/`,
  `internal/logto/`, `api/`, `examples/` or `test/integration/`, run `task it:all` (every version, one after the
  other); other pull requests run `task it` (newest).
- One job per workflow, the mise cache and path filters come from the private period (billed minutes); hosted
  minutes are free since the repository went public.
- Caches (mise, Go modules, Go build, golangci-lint) are saved on `main` only and restored on pull requests: a pull
  request can read `main`'s caches, never another pull request's. Go caches are pointed into `.cache/` of the workspace.
- No `pull_request_target`, read-only token, no secret in any pull request job, actions pinned by commit SHA.

## Environments

- None: nothing is deployed from this repository yet.

## Release

- Not set up yet. The image is built by ko (`.ko.yaml`); `task image:build` (in `task check`) builds every platform
  without pushing, `task image` loads one into the local Docker daemon.
