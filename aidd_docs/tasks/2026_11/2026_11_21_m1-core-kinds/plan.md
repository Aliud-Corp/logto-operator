---
objective: "`logtoctl plan|apply|check|render` reconciles the M1 core kinds of a `LogtoConfig` against every Logto of the support window (1.44, 1.45, 1.46): the reference configuration is created on an empty Logto, a second apply writes nothing, check exits 0 then 2 after a manual edit, and no secret ever reaches argv, stdout or logs."
status: pending
---

<!-- Fill or omit these sections; never add, rename, or reorder one. -->

# Plan: M1 — core kinds through the CLI, on a local Logto

## Overview

| Field      | Value |
| ---------- | ----- |
| **Goal**   | The engine and the CLI for connectors, custom profile fields, sign-in experience, resources and scopes, organization scopes and roles, applications (M2M secret, role grants) and application-level admin users, proven on the in-process fake and on a real Logto of each supported version. |
| **Source** | `docs/plan.md` section 11 (M1), with sections 2-7, 9 and 10; decisions of 2026-11-21 (API group, Q4, Q5, Q6, integration harness, Logto support window, kind-owned wire types). |

## Phases

| #   | Phase | File |
| --- | ----- | ---- |
| 1   | Support window and integration harness: three vendored specs, a real Logto of each version locally and in CI | [`phase-1.md`](./phase-1.md) |
| 2   | `LogtoConfig` schema, generated CRD and JSON Schema, loader and its rules | [`phase-2.md`](./phase-2.md) |
| 3   | Logto client, contract tests, capability check, engine core (plan, apply, verify, masking) | [`phase-3.md`](./phase-3.md) |
| 4   | Kinds: resources and scopes, organization scopes and roles, custom profile fields | [`phase-4.md`](./phase-4.md) |
| 5   | Kinds: SMTP connector and sign-in experience | [`phase-5.md`](./phase-5.md) |
| 6   | Kinds: applications (secrets, role grants) and admin users; outputs | [`phase-6.md`](./phase-6.md) |
| 7   | CLI commands and the M1 acceptance scenarios | [`phase-7.md`](./phase-7.md) |

## Decisions

| Decision | Why |
| -------- | --- |
| Logto support window: the latest minor and the two before it (1.44, 1.45, 1.46 today), each spec vendored as served by that version; integration tests on the vendored latest for pull requests and on all three on `main` | Logto ships a minor a month and fixes only its latest; measured 2026-11-21: 1.44 to 1.46 broke 7 operations, none used by M1, and the generator chain builds on 1.46 unchanged. |
| Kinds own their wire types: each kind declares the request and response structs of the fields it manages (optional, omitted when undeclared) and sends them through the generated client's raw-body variants; the generated client provides routes and parameters only. **This relaxes the initial decision** that request and response types are generated from the spec (user decision, 2026-11-21, option E) | One client and no per-version code: fields added or removed across the window are handled by the kind's struct, not by N generated clients or a spec merge. |
| Contract tests validate every kind's request bodies and recorded responses against each vendored spec of the window (kin-openapi schema validation) | Replaces the type safety the generated bodies gave: a field unknown to, or shaped differently in, a supported version fails a unit test, per version. |
| Capability check at plan time: the engine reads the target instance's own `/api/swagger.json` and refuses a declared field its Logto does not have | Logto exposes no version through the API (its spec says `info.version: Cloud`); the instance's spec is the authoritative list of what it accepts. |
| API group `logto.aliud.fr`, `apiVersion: logto.aliud.fr/v1alpha1`, kind `LogtoConfig` | Chosen 2026-11-21 (Q3). Forward DNS of a domain the studio owns; written in every configuration file, costly to change after M1. |
| `LogtoConfig` is a Kubernetes-shaped type (`TypeMeta`, `ObjectMeta`, `spec`) from M1, with kubebuilder markers; `controller-gen` (pinned in mise) generates deepcopy, the CRD YAML, and the JSON Schema of the CLI file is extracted from the CRD | One schema for the CLI file and the CR (decided), generated and drift-guarded, never written twice. |
| YAML decoded with `sigs.k8s.io/yaml` in strict mode (unknown fields are errors) | Same JSON tags as the CRD; a typo in a key must fail at load time, not silently do nothing. |
| `${var}` interpolation limited to keys declared in `spec.variables`, in string fields only; an unknown variable is an error (Q4) | Explicit, visible in `render`, no regex over every string. |
| Write-only secrets (Q5): measure first on a real Logto what `GET /api/connectors/{id}` returns and whether `PATCH` merges or replaces `config`, then implement the comparison that the measurement allows | The drift rule depends on a fact not yet observed; guessing it would either write on every apply or miss drift. |
| E-mail templates live in the SMTP connector `config.templates` in M1; `/api/email-templates` waits for M2 (Q6) | One mechanism per milestone. |
| One `test/integration/compose.yaml` (Postgres + Logto, version from `LOGTO_VERSION`, with its seeding entrypoint) used by `task it:up` locally and on the GitHub-hosted runner | Same command everywhere; a service container cannot override Logto's entrypoint. |
| Integration tests authenticate through the `m-default` route: its secret is read from the throwaway Logto's database by the harness (read only), token at the admin endpoint | Exercises the documented bootstrap route; no credential is created by hand and none exists outside the throwaway instance. |
