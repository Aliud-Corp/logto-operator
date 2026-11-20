# Deployment

## Pipeline

- GitHub Actions on GitHub-hosted runners only (`ubuntu-<version>`, pinned): the repository will be public, and a
  fork's pull request must never run on self-hosted infrastructure. `task ci:runners` fails on any other `runs-on`.
- `ci.yml`: one `check` job running `task check`.
- While private, minutes come out of the organization's shared free quota, and an exhausted quota blocks every hosted
  job of the organization until the next month: keep one job per workflow and the mise cache.
- No `pull_request_target`, read-only token, no secret in any pull request job, actions pinned by commit SHA.

## Environments

- None: nothing is deployed from this repository yet.

## Release

- Not set up yet.
