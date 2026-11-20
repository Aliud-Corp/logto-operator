# Logto Management API — vendored OpenAPI documents

One file per Logto OSS version of the **support window**: the latest minor and the two before it, listed newest first
in [`versions.txt`](versions.txt). Each `<version>.openapi.json` is the document that version serves at
`GET /api/swagger.json`, sorted and compacted with `jq -Sc`, dumped from a throwaway local Logto started with
`ENDPOINT=http://localhost:3001`. Never edit one by hand: a hand-edited spec is a second source of truth.

- The **newest** generates the client (`internal/logto/mgmt`), used for routes and parameters.
- **All of them** feed the contract tests: every request body a kind can send, and every response it decodes, is
  validated against each version of the window.

Two things in them that look wrong and are not:

- `info.version` says `Cloud`: that is what Logto OSS emits. Logto exposes no version number through its API.
- `servers[0]` echoes the host of the request that fetched the document, which is why `task spec:refresh` always
  asks `localhost:3001`: the file then depends on the Logto version only.

Literal values are encoded as `"format": "\"Social\""` (zod literals). `tools/specnorm` rewrites them into one-value
`enum`s, plus a short, self-checking list of overlays for generator limits, into `build/` before `oapi-codegen` runs.

## Adding a Logto version, dropping the oldest

1. Vendor the new version (needs Docker; it restarts the throwaway Logto at that version):

   ```bash
   task spec:refresh VERSION=1.47.0
   ```

2. Diff it against the current newest, filtered on the operations the kinds use, and read every breaking change:

   ```bash
   task spec:diff FROM=1.46.0 TO=1.47.0
   ```

3. Put the new version first in `versions.txt`, remove the oldest line and its file, run `task generate`, fix what
   no longer compiles, then `task check` and the integration tests on every version of the window.
4. One commit, and a release note naming the new window.

A refreshed existing version must come out byte-identical: `task spec:refresh VERSION=1.46.0` followed by
`git diff --exit-code api/logto/` proves the vendored file is what that Logto serves.
