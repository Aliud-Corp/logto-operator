# CLI

## Commands

- `logtoctl version`: the only command implemented; `plan`, `apply`, `check`, `render` are designed in the plan.

## Interface

- Usage errors exit 2.
- Secrets are read from the environment or files, never from flags.

## Distribution

- `task build` writes a static binary (`CGO_ENABLED=0`) to `bin/logtoctl`, version stamped from `git describe`.
