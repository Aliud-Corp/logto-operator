# logto-operator — plan

Status: DRAFT for review, 2026-11-20. No engine code yet. Every "measured" below was run on 2026-11-20 against the
vendored spec, the pinned tools or a local cluster; every "unverified" is a claim to prove on a local Logto before
code depends on it.

Inputs: Logto's vendored OpenAPI document (`api/logto/`), what Logto 1.44.0 OSS was observed to do (each such fact
below is marked "observed" and gets an integration test before code relies on it), and the studio's engineering
conventions (task runner only, pinned tools, committed generated code guarded against
drift, tests first, guards that can fail).

## 1. Scope and non-goals

The tool owns the **configuration** of one Logto tenant (the `default` tenant of a self-hosted Logto OSS 1.44.0):
sign-in experience, SMTP connector and e-mail templates, custom profile fields, API resources and scopes, global
roles, organization scopes and roles, applications (SPA, M2M, Traditional, ...), webhooks, JWT customizer (if it
works on OSS, see 10), account center, captcha, and the application-level admin users (`platform_admin`-style).

Non-goals: organizations and users coming from sign-up (never created, never pruned); demo/fixture data (it stays with
each consuming project); anything that needs a database write (bootstrap of the first M2M application, declared
ids); the Logto deployment itself (the deployment repository owns it).

Decisions already taken (not reopened here): Go; one engine, a static CLI and an operator; generated typed client from
the vendored spec with a drift guard; one coarse `LogtoConfig` CRD that is also the CLI file format; no CronJob
(event-driven controller with in-process resync, read-only `check` in CI); ids are outputs, applications adopted by
name; credentials from a Secret reference; `plan` / `apply` / `check` / `render`, idempotent, secrets never in argv
or logs, an unchanged re-apply writes nothing; `prune` per kind, off by default.

## 2. Architecture

```
cmd/
  logtoctl/              CLI: plan | apply | check | render | version            (one static binary, CGO off)
  logto-operator/        controller manager (milestone 3)
api/
  logto/                 vendored Logto OpenAPI spec (source of the client)
  v1alpha1/              Go types of LogtoConfig = THE schema. CRD YAML and JSON Schema are generated from them
internal/
  logto/mgmt/            GENERATED client/types/chi server (oapi-codegen), never edited
  logto/logtofake/       fake Management API on the generated server (unit tests)
  logto/client/          the only door to Logto: token source (client_credentials, Basic auth, refresh before
                         expiry, optional distinct token endpoint for m-default), pagination (page_size 100, loops
                         until a short page), bounded retries on 5xx/429, errors that never carry a request body
  engine/                desired model -> observe -> diff -> plan -> apply; knows nothing of CLI or Kubernetes
  engine/kinds/<kind>/   one adapter per object kind (see the interface below); the ONLY code that touches mgmt types
  engine/plan/           Plan, Op (create|update|delete|noop), field-level Change, masking
  secrets/               SecretSource resolution: env/file for the CLI, Kubernetes Secret for the operator
  render/                variable interpolation, masked rendering
  outputs/               ids and secrets produced by an apply (CLI files/stdout, operator Secret/ConfigMap/status)
  controller/            controller-runtime reconciler wrapping engine (milestone 3)
test/
  e2e/                   Chainsaw suite on a throwaway k3d cluster (`task e2e`)
  integration/           engine against a real Logto 1.44.0 (build tag `integration`, milestone 1)
```

The kind adapter interface (sketch, not final):

```go
type Kind interface {
    Name() string                                   // "applications", "resources", ...
    DependsOn() []string                            // kinds whose outputs it reads (ids)
    Observe(ctx, c *client.Client) (Observed, error) // GET + normalize (sort sets, fill server defaults)
    Diff(desired Desired, observed Observed, refs Refs) ([]plan.Op, error)
    Apply(ctx, c *client.Client, op plan.Op) (Outputs, error)
}
```

`engine.Plan(desired)` runs Observe + Diff over the kinds in dependency order and returns a `Plan` without writing.
`engine.Apply(plan)` executes the ops in order, recording outputs (ids) that later kinds read, then re-observes the
touched kinds and fails if Logto did not keep what was written.
`check` = `Plan` + exit code. The CLI and the controller are thin shells around these two calls.

Generator choice, **measured**: `oapi-codegen` 2.8.2 (pinned in `mise.toml`, mise `go` backend). `ogen` 1.25.0 was
tried on the same spec and **skipped 46 operations**, among them `GetSignInExp`, `UpdateSignInExp`, every connector
route, `CreateCustomProfileField`, `UpdateCustomProfileFieldByName`, the JWT customizer routes, `CreateUser` and
`UpdateUser` (unsupported `oneOf` shapes): disqualifying. oapi-codegen generates all 245 paths after two documented
fixes in `tools/specnorm` (Logto's literal-in-`format` encoding, one naming collision overlay that fails loudly if its
target moves). Its costs, accepted: anonymous inline structs (the adapters map them to our own types, so they never
leave `engine/kinds`), `number` -> `float32`, ~7 MB of generated code (collapsed in diffs by `.gitattributes`). Its
chi server comes with an `Unimplemented` base, which is what makes the fake answer 501 on any route nobody modelled.

## 3. Desired-state schema (sketch)

One document per Logto instance. The CLI reads it as YAML; the operator reads the same type as a CR. Objects that
reference each other are listed under a **local key** (a map key, stable, ours) distinct from the Logto `name`
(theirs, displayed in the console), so a rename in Logto is an update, not a delete + create.

```yaml
apiVersion: logto.<studio-domain>/v1alpha1        # group: open question 3
kind: LogtoConfig
metadata:
  name: example
spec:
  endpoint: https://auth.stag.example.com             # where the Management API answers
  credentials:                                      # Management API M2M client
    clientId:     {valueFrom: {secretKeyRef: {name: logto-mgmt, key: clientId}}}   # operator
    clientSecret: {valueFrom: {env: LOGTO_MGMT_CLIENT_SECRET}}                     # CLI: env or file only
    tokenEndpoint: https://logto.stag.example.com     # optional: the console endpoint for the m-default route
  variables:                                        # open question 4: interpolation vs overlays
    zone: stag.example.com
  prune: {resources: false, organizationRoles: true, webhooks: false}             # per kind, all false by default

  signInExperience:                                 # only declared keys are owned, compared and patched
    color: {primaryColor: "#5B4FE0", isDarkModeEnabled: false}
    languageInfo: {autoDetect: false, fallbackLanguage: fr}
    branding: {logoUrl: {fromFile: sign-in-experience/logo.svg, as: dataUri}}
    customCss: {fromFile: sign-in-experience/custom.css}
    signUp: {identifiers: [email], password: true, verify: true}
    signIn: {methods: [{identifier: email, password: true, verificationCode: true, isPasswordPrimary: true}]}
    unknownSessionRedirectUrl: https://app.${zone}
  connectors:
    smtp:
      id: example-smtp                              # POST /api/connectors accepts a declared id (read in the spec)
      connectorId: simple-mail-transfer-protocol
      config:
        host: smtp.example.com
        port: 587
        auth: {user: {valueFrom: {env: LOGTO_SMTP_USER}}, pass: {valueFrom: {env: LOGTO_SMTP_PASS}}}
        fromEmail: no-reply@${zone}
        templates: {fromDir: email-templates/}      # open question 6: connector templates vs /api/email-templates
  customProfileFields:
    fullname: {type: Fullname, label: Nom complet, required: true, config: {parts: [...]}}
  resources:
    app:
      name: Example App API
      indicator: https://app.${zone}/api
      accessTokenTtl: 3600
      scopes: {"event:read": {description: ...}}
  roles:                                            # global roles (User and MachineToMachine)
    platformAdmin: {name: platform_admin, type: User, scopes: [{resource: admin, scope: all}]}
  organizationScopes: {"members:read": {description: ...}}
  organizationRoles:
    owner:
      name: organization_owner
      organizationScopes: ["members:read"]
      resourceScopes: [{resource: app, scope: "event:read"}]
  applications:
    organizer:
      name: Example App
      type: SPA                                     # immutable in Logto: a change is a plan error
      oidcClientMetadata: {redirectUris: [https://app.${zone}/callback], postLogoutRedirectUris: [https://app.${zone}]}
      customClientMetadata: {corsAllowedOrigins: [https://app.${zone}], alwaysIssueRefreshToken: true}
    backend:
      name: Example Backend
      type: MachineToMachine
      roles: [{builtin: "Logto Management API access"}]   # granted through /api/roles/{id}/applications
      secret: {publish: true}                       # ensure one usable secret exists, publish it, never rotate
    admin:
      name: Example Admin
      type: SPA
      accessControl: {userRoles: [platformAdmin]}   # rule first, then enable (Logto answers 422 otherwise)
  webhooks:
    passwordReset: {name: ..., events: [PostResetPassword], url: https://app.${zone}/internal/logto/hooks, publishSigningKey: true}
  jwtCustomizer:
    accessToken: {script: {fromFile: jwt/access-token.js}, keepEnvironmentVariables: [SSO_DOMAINS]}
  users:                                            # application-level admins only; matched by primaryEmail
    teamAdmin: {primaryEmail: admin@example.com, roles: [platformAdmin]}   # never a password in the file
  outputs:                                          # what to publish, by local key
    configMap: {name: example-logto-ids}            # operator only
    secret: {name: example-logto-secrets}
status:                                             # operator only
  observedGeneration: 3
  appliedHash: sha256:...
  conditions: [{type: Ready}, {type: Drifted}, {type: Degraded}]
  sections: {applications: {state: InSync, lastAppliedAt: ...}}
  outputs:
    applications: {organizer: {id: generated0000example1}}
    resources: {app: {indicator: https://app.stag.example.com/api}}
```

`fromFile` / `fromDir` are resolved by the CLI relative to the document; for the operator the same document is
**rendered** first (`logtoctl render --inline`, files inlined, secrets left as references) so the CR is
self-contained. Schema rules enforced by the loader (not by Logto three calls later): unique Logto names per kind,
every reference resolves, scope names have no whitespace, `port` is a number, an SMTP config carries the three
mandatory templates (`Register`, `SignIn`, `ForgotPassword`) and non-empty `auth` (observed: Logto accepts the
config otherwise and the sign-up screen fails later),
audiences of distinct resources differ (a backend that verifies `aud` cannot tell two equal ones apart).

## 4. Reconciliation order and dependency rules

Order of one apply (prune runs afterwards, in the reverse order, only for kinds with `prune: true`):

1. **connectors** — an e-mail sign-up identifier with `verify: true` is refused without an e-mail connector
   (`passwordless_requires_verify`).
2. **customProfileFields** — before the sign-in experience that may order them (`/properties/sie-order`).
3. **signInExperience** — singleton, `PATCH` of the declared keys only.
4. **resources**, then their **scopes**.
5. **roles** (global, User and M2M) with their resource scopes.
6. **organizationScopes**, then **organizationRoles** (scopes and resource scopes `PUT` = the declared set).
7. **applications** — adopt by name or create; then M2M **secrets**, **role grants**, **access control** (needs 5).
8. **webhooks** (signing key read back, never rotated by a replay).
9. **jwtCustomizer**, **idToken** extended claims, **accountCenter**, **captcha**.
10. **users** (application-level admins): matched by `primaryEmail`, roles granted; never a password, never pruned.
11. **outputs** published (ids first, then secrets).

Dependency rules:

- Identity (how an observed object is matched to a declared one): resources by `indicator`; scopes by `name` within
  their resource; roles by `name` + `type`; organization scopes/roles by `name`; applications by `name` (two Logto
  applications with the same name = plan error, never a guess); connectors by declared `id`, else by factory
  `connectorId` when exactly one exists (adoption); webhooks by `name`; profile fields by `name`;
  users by `primaryEmail`.
- Ids are **only** read from the observed state or from an op's response, never declared. A reference to an object
  that the same plan creates is resolved at apply time (the plan shows `(known after apply)`).
- Set-valued fields (redirect URIs, scope lists, role scopes, hook events) are compared as sets; ordered ones
  (sign-in methods) as lists. Server defaults are normalized away before diffing so a field Logto fills in does
  not look like drift.
- Ownership is per declared field: an undeclared field of an owned object is left as Logto has it, except where a
  sub-resource is declared as a whole (an organization role's scope set IS the declaration).
- Refusals are early: an immutable change (application `type`), an ambiguous match, a reference to nothing, or a
  scope removal that would revoke tokens without `prune` stop the plan before any write.

## 5. Drift, plan and prune

- `plan` prints, per kind, `+ create`, `~ update` (field-level, secrets masked as `(sensitive)`), `- delete` (prune),
  and an unchanged count. `check` = `plan`, exit **0** in sync, **2** drift, **1** error; `apply` exits **3** when it
  completed with warnings.
- A re-apply of an unchanged document issues **zero writes** (tested: the fake counts non-GET calls).
- **Write-only secrets** are the hard case: Logto may not return the SMTP `auth.pass` (unverified). If Logto returns
  it, compare it. If not, the engine cannot know whether it changed; options in open question 5.
- **Prune** is per kind, off by default, and never applies to users, organizations, Logto built-ins (the Management
  API resource `https://default.logto.app/api`, the `Logto Management API access` role, `m-default`/admin apps),
  nor to objects listed in `spec.prune.keep`. Logto has no "managed-by" marker on most objects and the tool keeps no
  state file, so prune means "delete what this kind holds and the document does not declare": that is why it is
  opt-in per kind. Resource scopes are never pruned unless asked (removing one revokes it from every token). In the
  operator, deleting the CR does **not** delete anything in Logto (open question 7).

## 6. Secrets and ids publication

- CLI: secrets come from environment variables or files (`valueFrom: {env}` / `{file}`), never flags. stdout carries
  identifiers only (`KEY=value`); secrets produced by an apply (M2M client secrets, webhook signing
  keys) go to `--secrets-out <file>` (created 0600, mode enforced on an existing file) or `--secrets-fd <n>`, never
  to stdout unless `--print-secrets`. `--outputs json` writes ids as JSON for project tooling.
- Operator: a ConfigMap (ids, indicators, endpoints) and a Secret (M2M secrets, signing keys), both owned by the CR
  (ownerReference, garbage-collected with it), names from `spec.outputs`; `status.outputs` repeats the ids (never a
  secret) so other manifests can read them.
- An M2M secret is never generated or rotated by a replay: if the application has no usable secret, one is created
  (`POST /api/applications/{id}/secrets`, Logto generates the value) and read back (`GET .../secrets`); otherwise the
  existing one is published. Logto does **not** let a client choose a secret value (measured: the POST body only has
  `name` and `expiresAt`), so a secret a project declares today in its own configuration becomes a published output.
- Redaction is structural: the client never logs or wraps request bodies; `render` masks every `valueFrom` and every
  field the schema marks sensitive; a test greps all outputs (stdout, stderr, logs, events, status) for the secret
  values used by the test and fails on a hit.

## 7. Credentials and bootstrap

Management API credentials = an M2M client holding `Logto Management API access`, token by `client_credentials` for
resource `https://default.logto.app/api`, scope `all` (OSS has no narrower scope: least privilege is by identity).
Two supported routes, both a one-time operator step per environment, documented in `docs/bootstrap.md` (M1):

1. **The `m-default` route** (no database write): Logto's built-in M2M app of the **admin tenant**; its secret is
   read once from the database (`select secret from applications where id = 'm-default'`) or the route is skipped;
   token at the **console endpoint** (`tokenEndpoint`), API calls at the tenant endpoint.
2. **A dedicated M2M app created in the console** (or by a first `logtoctl apply` running as `m-default`), with the
   role; token at the tenant endpoint. Recommended for steady state: one revocable identity per environment and per
   consumer (CI check, operator), never the backend's own.

The tool never creates its own first credential and never touches the database.

## 8. Admin console users (investigation, read-only)

What the sources say:

- The vendored spec is the `default` tenant's Management API; it has no admin-tenant route, and a default-tenant
  token cannot reach the admin tenant (observed).
- Console users live in the **admin tenant** (observed in its data model): a user of `tenant_id = 'admin'` holding
  the admin-tenant roles `user` and `default:admin`, member of the admin-tenant organization `t-default` with
  organization role `admin`; the admin tenant's sign-in mode is `SignIn`.
- The admin tenant runs the same Management API code. From memory of Logto's source (**unverified**): it is served
  at the console endpoint, its resource is `https://admin.logto.app/api`, and a built-in M2M app `m-admin` holds
  access to it, the way `m-default` holds access to the default tenant.

Verdict: **feasible in principle, unproven.** Through the admin tenant's Management API, that data model maps to four
documented calls (`POST /api/users`, `POST /api/users/{id}/roles`, `POST
/api/organizations/t-default/users`,
`PUT .../users/{id}/roles`). It needs a second credential (`m-admin`, read once from the database, same class as the
`m-default` bootstrap) whose blast radius is every tenant's console. Not scheduled before a spike on a local Logto
proves the route (milestone 5, or dropped: open question 9). Application-level admins (`platform_admin`) are ordinary
default-tenant users and are in scope from milestone 2.

## 9. Test strategy

1. **Unit, engine on the fake** (`internal/logto/logtofake`): the fake implements `mgmt.ServerInterface`, every
   unmodelled route answers 501; it models observed Logto 1.44.0 behaviours (422 when enabling access
   control without a rule, `page_size` > 100 refused, default page of 20, connector `config` replace-on-PATCH once
   measured). Table tests per kind: create, adopt, update one field, no-op, ambiguous match, prune on/off. Property
   asserted everywhere: **apply twice, the second plan is empty and the fake saw zero writes**.
2. **Contract**: the fake validates every request body the engine sends against the normalized spec (kin-openapi
   request validation middleware), so an engine that builds a body Logto would reject fails in unit tests.
3. **Behaviour scenarios**, run on the fake and again on a real Logto (4), against the reference configuration
   `examples/reference.yaml` (every kind, committed): first run creates everything; replay changes nothing,
   secrets included; drift repaired without touching secrets (a redirect URI, a hook URL); JWT environment
   variables kept as found; ids only on stdout, secrets only in a 0600 file, never on stdout/stderr; a wrong
   credential fails clearly; two resources with the same audience are refused.
4. **Integration, real Logto 1.44.0** (`test/integration`, build tag): the same scenarios against a fresh Logto, plus
   what only a real Logto can tell (read-back of secrets, PATCH merge vs replace, JWT customizer execution by
   minting and decoding a real token, which endpoints of a "Cloud" spec actually work on OSS). In CI (GitHub-hosted
   runner, section 12b): Postgres as a job service container, Logto started by a `docker run` step with its seeding
   entrypoint (`npm run cli db seed -- --swe && npm run alteration deploy latest && npm start`), because a service
   container cannot override the entrypoint. No secret involved: the bootstrap
   credential of that throwaway Logto is created by the job itself.
5. **End-to-end, Chainsaw on k3d** (`test/e2e`, `task e2e`): the operator deployed in a throwaway k3d cluster with
   Logto + Postgres in it; scenarios as Chainsaw tests (apply a `LogtoConfig`, assert `status` conditions and the
   published Secret/ConfigMap, edit Logto behind the operator's back and assert `Drifted` then repaired, delete the
   CR and assert Logto untouched). **Today:** harness only (pinned k3d 5.9.0, k3s `v1.37.2-k3s1`, chainsaw 0.2.15,
   kubectl 1.37.2, one smoke test) — **measured green locally** (cluster up, test passed, cluster down), run in CI
   by the path-filtered `e2e` workflow on a GitHub-hosted runner.
6. **Guards**: `task generate:check` (always regenerates, fails on drift; proven to fail on a tampered file),
   `task ci:runners` (GitHub-hosted `ubuntu-<version>` only; proven to fail on a self-hosted and on an unpinned
   label), `go mod tidy -diff`, golangci-lint, actionlint + shellcheck. Each new guard is shown failing once in its PR.

## 10. OSS 1.44.0 API gaps (what the Management API cannot do, or not reliably)

| # | Gap | Evidence | Consequence |
| --- | --- | --- | --- |
| 1 | Application id cannot be chosen (`POST /api/applications` has no `id`) | spec, measured | ids are outputs; adoption by name; projects read published ids |
| 2 | M2M secret value cannot be chosen (`POST .../secrets` takes `name`, `expiresAt` only) | spec, measured | secrets are outputs, never rotated by a replay |
| 3 | Organization id cannot be chosen (`POST /api/organizations` has no `id`) | spec, measured | out of scope anyway (organizations come from sign-up) |
| 4 | No first credential without a credential | by construction | bootstrap is a manual step (section 7) |
| 5 | Admin tenant (console users) unreachable with a default-tenant token | observed | section 8 |
| 6 | JWT customizer: whether a self-hosted 1.44.0 actually runs the stored script at token issuance is **unproven** (observations disagree); `/api/configs/jwt-customizer/test` only proves the script runs | observed, conflicting | supported as a kind, gated by an integration test minting a real token; the `/test` endpoint proves the script runs, not that issuance uses it |
| 7 | Connector `config` PATCH: merge or replace unknown; `GET /api/connectors/{id}` may or may not return `auth.pass` | unknown | assume replace (always send the full config incl. secrets); drift on write-only secrets: open question 5 |
| 8 | No "managed-by" marker on resources, scopes, roles; no state file | spec | prune is opt-in per kind with a keep list |
| 9 | Spec says `info.version: Cloud`: presence of a route is not proof it works on OSS (custom UI assets zip, domains, SAML apps, sentinel...) | spec | each kind enters scope only with an integration test |
| 10 | Two e-mail template mechanisms: connector `config.templates` and `/api/email-templates` (per language) | spec | open question 6 |
| 11 | Lists default to 20 and cap `page_size` at 100 | observed | client always paginates |
| 12 | Built-in role found by name (`Logto Management API access`) | observed | a rename in a Logto upgrade fails loudly at plan time |
| 13 | Application `type` immutable | observed | plan error, manual migration |
| 14 | Custom phrases (`/api/custom-phrases/{languageTag}` PUT) exist in the spec; never exercised | spec | candidate kind after an integration test |

## 11. Milestones

- **M0 — plan and skeleton.** Four pull requests: this plan, the agent memory and the contributor docs; the pinned
  toolchain, generated client, drift and runner guards and the `check` CI; the fake Management API with its
  hello-world test and the CLI stub; the Chainsaw/k3d harness with a smoke test and its `e2e` workflow.
- **M1 — core kinds through the CLI, on a local Logto.** `api/v1alpha1` types (generated JSON Schema), loader with
  the schema rules of section 3, `internal/logto/client`, kinds: connectors, customProfileFields, signInExperience,
  resources + scopes, organizationScopes, organizationRoles, applications (+ M2M secret, role grants), users
  (application admins, no passwords); `plan` / `apply` / `check` / `render`; `examples/reference.yaml` covering
  those kinds. Acceptance: on an empty local Logto 1.44.0, `apply` of the reference configuration creates every
  declared object (asserted object by object through the API), a second `apply` writes nothing, `check` exits 0
  then 2 after a console edit, `render` shows no secret. Integration test harness (`task it`) run locally and by a
  CI job against a Logto started in the job.
- **M2 — remaining kinds.** Global roles, access control, webhooks, JWT customizer + id token, account center,
  captcha, `/api/email-templates`; the behaviour scenarios of section 9 complete; outputs files.
- **M3 — operator.** CRD generated from `api/v1alpha1` (controller-gen, committed, drift-guarded), controller-runtime
  manager, Secret watch, resync (`RequeueAfter`, default 10 min, no CronJob), conditions, outputs Secret/ConfigMap,
  `spec.mode: Observe|Apply` (Observe = plan only, status `Drifted`), narrow RBAC, manifests (kustomize, committed);
  Chainsaw e2e with Logto inside k3d.
- **M4 — first adopters.** A consuming project renders its `LogtoConfig` and runs `check` in its CI; each
  environment starts in `Observe` (drift reviewed, anything edited by hand in a console shows up), then `Apply`.
- **M5 — admin console users** (only if section 8's spike succeeds).

## 12. Release and versioning

- License: Apache-2.0 (the Kubernetes ecosystem's default; explicit patent grant).
- SemVer tags `vX.Y.Z` (Go modules require SemVer: a deliberate deviation from the studio's CalVer tags).
  `v0.x` until M4. The supported Logto version is part of the release notes; a Logto upgrade = new spec, regenerate,
  minor (or major if the schema breaks) release.
- Binary: goreleaser, `linux` / `darwin` × `amd64` / `arm64`, CGO off, checksums and SBOM, attached to the GitHub
  release; `logtoctl version` prints the tag and the Logto version it targets.
- Image: built with **ko** (Go-native, reproducible, no Dockerfile), distroless static base, multi-arch. Registry:
  `ghcr.io/<owner>/logto-operator` (public repository, public image, pinned by digest by its consumers). Promotion
  re-tags, never rebuilds. Release workflows publish with the workflow's `GITHUB_TOKEN` (`packages: write`) on tag
  pushes only, never from a pull request.

### 12b. CI runners (decided 2026-11-20)

CI runs on **GitHub-hosted runners** (`ubuntu-<version>`, pinned), which carry a Docker daemon (k3d, Logto via
`docker run`). Never the studio's self-hosted runners: the repository will be **public**, and a fork's pull request
would run arbitrary code on studio infrastructure. `task ci:runners` (in `task check`) fails on any other `runs-on`.

The repository starts **private** and goes public later. While private, its minutes count against the organization's
shared free quota, and an exhausted quota blocks every hosted job of the organization until the next month. So: one
`check` job running the whole gate (jobs are billed rounded up to the minute), the mise
cache, the e2e suite in its own path-filtered workflow, nothing on draft pull requests, superseded runs cancelled.
Once public, minutes are free: the gate can be split back into readable jobs.

Rules: no `pull_request_target`; `permissions: contents: read` by default; no secret in any pull request job (the
integration and e2e Logtos are throwaway and local to the job); third-party actions pinned by commit SHA. Blacksmith
was considered (faster, Docker layer cache) and not taken: one more vendor with an org-wide GitHub App, for a CI this
small; switching later is a one-line `runs-on` change.

## 13. Open questions

1. ~~e2e cluster in CI~~ — **decided**: GitHub-hosted runners, k3d in the runner's Docker (12b).
2. ~~Real Logto in CI~~ — **decided**: Postgres service container + Logto by `docker run` in the job (9.4, 12b).
3. **Names.** CLI `logtoctl`, operator `logto-operator`, CRD group `logto.<domain>`: which studio domain?
4. **Environment-specific values.** `${var}` interpolation in the document (explicit, limited to
   declared variables) vs one fully-rendered document per environment (kustomize overlays on the
   CR). My pick: interpolation, limited to declared `variables`, rendered and shown by `render`.
5. **Write-only secrets drift** (SMTP password, if Logto does not return it): (a) store a salted hash in the object's
   `customData`/`metadata` where Logto offers one and compare that; (b) always re-send (breaks "unchanged = zero
   writes" for that one field, reported as such); (c) ignore. Decide after measuring what Logto returns (M1).
6. **E-mail templates.** Connector `config.templates` or `/api/email-templates` (per language), or both as
   two kinds?
7. **CR deletion.** Leave Logto untouched (my pick, no finalizer) or prune everything the CR declared?
8. **JWT customizer** (gap 6): keep it in M2 behind an integration test that mints a real token and decodes it?
9. **Admin console users** (section 8): spike in M5, or drop the requirement given the `m-admin` blast radius?
10. **Where `LogtoConfig` CRs live**: rendered by each project and bumped into the deployment repository, or
    authored there. It decides who consumes the published ids.
11. **First run per environment in `Observe` mode** (plan only, no write) before switching to `Apply`: make it the
    operator default?
