# Deployment

## Pipeline

- GitHub Actions on GitHub-hosted runners only (`ubuntu-<version>`, pinned): the repository is public, and a
  fork's pull request must never run on self-hosted infrastructure. `task ci:runners` fails on any other `runs-on`.
- `ci.yml`: one `check` job running `task check`, a required status: it runs on every pull request, and skips the
  gate when only Markdown changed (`task ci:docs-only`), since a path-filtered workflow reports no status at all.
- `e2e.yml`: `task e2e`, path-filtered on what it exercises.
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

- release-please (`release.yml`, push to `main`): one release pull request; merging it tags `vX.Y.Z` (`go`
  release type, `0.x` minor on a breaking change) and creates the GitHub release.
- The release pull request is opened with a GitHub App's token (`RELEASE_APP_ID`, `RELEASE_APP_PRIVATE_KEY`): the
  organisation forbids `GITHUB_TOKEN` to open pull requests, and its pull requests would trigger no CI.
- `publish` runs `task release` on the tag: ko pushes the image to `ghcr.io/aliud-corp/logto-operator` and pins it in
  `build/release/install.yaml`; GoReleaser (`.goreleaser.yaml`) builds the `logtoctl` archives and `checksums.txt`
  and uploads them with `install.yaml` to the release (`keep-existing`: the notes stay release-please's).
  `GITHUB_TOKEN`, `packages` and `contents` write, never on a pull request. `task release:snapshot` builds the
  archives locally without publishing.
- `task image:build` (in `task check`) builds every platform without pushing; `task image` loads one into the local
  Docker daemon.
