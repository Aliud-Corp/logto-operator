# CLI

## Commands

- `logtoctl render | plan | check | apply -f FILE`, and `version` (`internal/cli`; the binary is `cmd/logtoctl`).
- `-f -` reads stdin (files relative to the working directory); a stream of several documents is refused.
- Every command loads and validates the document first; `plan`, `check` and `apply` then read the target's own spec
  (capability check) and run every kind (`internal/engine/kinds.All`).

## Interface

- Exit codes: 0 done or in sync, 1 error, 2 drift (`check`) or usage error, 3 applied with secrets written nowhere.
- `apply`: the plan it ran on stderr, identifiers on stdout (`--outputs env|json`), secrets only to `--secrets-out`
  (0600) or `--secrets-fd`, on stdout only with `--print-secrets`.
- An untested Logto (spec none of the vendored ones) is refused unless `--allow-untested-logto`.
- An interrupted apply prints `stopped: N applied, failed <kind key>, M not run`, and says when a retry may help.
- Credentials and secrets come from the document's `valueFrom` (env, file), never from a flag; every error message
  goes through the redactor.

## Distribution

- `task build` writes a static binary (`CGO_ENABLED=0`) to `bin/logtoctl`, version stamped from `git describe`.
- People-facing usage: `docs/usage.md`.
