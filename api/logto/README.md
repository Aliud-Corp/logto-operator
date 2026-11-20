# Logto Management API — vendored OpenAPI document

`management.openapi.json` is the OpenAPI 3.0.1 document of Logto's Management API, **Logto OSS 1.44.0**
(`ghcr.io/logto-io/logto:1.44.0`), as the running server publishes it at `GET /api/swagger.json`, sorted and compacted
with `jq -Sc`. It is the **source** of the generated client `internal/logto/mgmt` (and of the fake Management API of
the tests). Never edit it by hand: a hand-edited spec is a second source of truth.

Provenance of the current file: dumped from a local Logto 1.44.0 started with
`ENDPOINT=http://localhost:3001`, sha256
`62662d2835bbc6e1ecf5935ed27ec4cb8df1aaaec28955b75453d14c148c76a5`.

Two things in it that look wrong and are not:

- `info.version` says `Cloud`, and `servers[0]` is the endpoint it was dumped from: that is what Logto 1.44.0 OSS emits. The client takes its endpoint at run time; `servers` is unused.
- Literal values are encoded as `"format": "\"Social\""` (zod literals). `tools/specnorm` rewrites them into
  one-value `enum`s, plus a short, self-checking list of overlays for generator limits, into `build/` before
  `oapi-codegen` runs. The vendored file itself is never rewritten.

## Refreshing it (a Logto upgrade)

1. Start the target Logto version locally (never a deployed one), e.g. the image `ghcr.io/logto-io/logto:<version>`.
2. Dump and normalise the spec in place:

   ```bash
   curl -sSf http://localhost:3001/api/swagger.json | jq -Sc . > api/logto/management.openapi.json
   ```

   Check that `paths` is not empty before committing: an empty spec is valid JSON and passes every guard.
3. `task generate`, then fix whatever no longer compiles (engine, fake). A `tools/specnorm` overlay whose target moved
   fails the generation on purpose: remove or update it.
4. `task check`, then update the Logto version everywhere it is pinned (this README, docs/plan.md, CI service
   containers, the e2e manifests) in the same commit.

A dedicated `task spec:refresh` (dump from a local Logto started by the integration harness) arrives with milestone 1.
