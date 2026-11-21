# Architecture

## Stack

- Go: one static binary for the CLI, the cluster-operator norm for the controller.
- `oapi-codegen` generates the Management API client, types and a chi server from Logto's vendored OpenAPI spec.
- `mise` pins every tool, `task` is the only entry point.

## How it fits together

```mermaid
flowchart LR
    spec["api/logto spec"] --> norm["tools/specnorm"] --> gen["internal/logto/mgmt (generated)"]
    gen --> fake["internal/logto/logtofake"]
    doc["LogtoConfig document"] --> config["internal/config"] --> engine["internal/engine + kinds"]
    gen --> engine
    engine --> client["internal/logto/client"] --> logto[("Logto Management API")]
    cli["cmd/logtoctl"] --> engine
```

## Key decisions

- Management API only, never a database write: whatever the API cannot do (first credential, declared ids) stays a
  manual step.
- The vendored spec is the source and is never edited; generator workarounds live in `tools/specnorm` (literal
  `format` rewrite, overlays that fail when their target moves).
- `oapi-codegen` over `ogen`: `ogen` skipped 46 operations of Logto's spec, including sign-in experience, connectors
  and custom profile fields.
- Generated code is committed and guarded by `task generate:check`, which always regenerates.
- Engine first, operator second: the CLI and the controller are thin shells over the same engine.
- One schema, `logto.aliud.fr/v1alpha1` (`api/v1alpha1`): the Go types generate the CRD (controller-gen) and the
  JSON Schema of the CLI file (`tools/crd2schema`); nothing is written twice.
- Kinds own their wire types (only the fields they manage) and send them through the generated request builders
  (`client.Do`); contract tests validate them against every vendored spec of the window (`internal/logto/contract`),
  and the engine refuses at plan time a field the target's own spec lacks (`internal/logto/capability`).
- The engine (`internal/engine`) runs kinds in `DependsOn` order: Plan observes and diffs; Apply runs the ops, then
  plans the kind again and fails if Logto did not keep the write. No state file: Logto is the state.
- A document is loaded strictly (`internal/config`): unknown keys fail with their path, `${name}` comes from
  `spec.variables` only, files are resolved relative to the document, then every schema rule runs at once.

## Gotchas

- Logto encodes zod literals as `"format": "\"Value\""`; the generator's loader refuses it raw.
- `oapi-codegen` maps OpenAPI `number` to `float32`: epoch milliseconds lose precision through those fields.
- The spec says `info.version: Cloud`: a route's presence does not prove it works on OSS.
- Logto writes `pattern` as JavaScript literals (`/^#...$/i`) and zod unions as `oneOf` meaning `anyOf`: the
  validation loader (`internal/logto/openapi`) converts both, or valid values are refused.
- Logto exports fields checked by a custom zod function (redirect URIs, names...) as empty objects "Validator
  function": `specnorm` leaves them unconstrained, or the generated client cannot decode real answers.
- YAML flow sequences break on `${...}`: quote such strings (`["https://app.${zone}"]`).
