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
works on OSS, see 10), account center, captcha.

Non-goals: users and organizations, whoever creates them (sign-up, invitations, the product itself, a fixture): they
are data, never created, updated or pruned, and neither are memberships or passwords (decided 2026-11-22, the `users`
kind of M1 is removed: a declared user's name was set back over the user's own edit at every run); demo/fixture data
(it stays with each consuming project); admin console users (section 8, manual for now); anything that needs a database write (bootstrap of the first M2M application, declared
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

**Revised 2026-11-21 (option E):** kinds own their request and response types (only the fields they manage) and
send them through the generated client's raw-body variants; the generated client provides routes and parameters.
Contract tests validate those types against the vendored spec of every Logto version in the support window (the
latest minor and the two before it). This replaces "request and response types generated from the spec": one client,
no per-version code. Generator choice, **measured**: `oapi-codegen` 2.8.2 (pinned in `mise.toml`, mise `go` backend). `ogen` 1.25.0 was
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
    color: {primaryColor: "#101418", isDarkModeEnabled: false}
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
    platformAdmin: {name: platform_admin, description: ..., type: User, scopes: [{resource: admin, scope: all}]}
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
      roles: [{name: "Logto Management API access"}]      # by Logto name: declared in roles, or held by Logto
      secret: {publish: true}                       # ensure one usable secret exists, publish it, never rotate
    admin:
      name: Example Admin
      type: SPA
      accessControl: {userRoles: [platformAdmin]}   # rule first, then enable (Logto answers 422 otherwise)
  webhooks:
    passwordReset: {name: ..., events: [PostResetPassword], url: https://app.${zone}/internal/logto/hooks, publishSigningKey: true}
  jwtCustomizer:
    accessToken: {script: {fromFile: jwt/access-token.js}, keepEnvironmentVariables: [SSO_DOMAINS]}
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
   **cimd** — singleton (Logto 1.44+): `PATCH /api/configs/cimd`, then the tenant-wide consent scopes (they name
   resource scopes, so after 4).
8. **webhooks** (signing key read back, never rotated by a replay).
9. **jwtCustomizer**, **idToken** extended claims, **accountCenter**, **captcha**.
10. **outputs** published (ids first, then secrets).

Dependency rules:

- Identity (how an observed object is matched to a declared one): resources by `indicator`; scopes by `name` within
  their resource; roles by `name` (a different `type` = plan error, Logto cannot change it); organization scopes/roles by `name`; applications by `name` (two Logto
  applications with the same name = plan error, never a guess); connectors by declared `id`, else by factory
  `connectorId` when exactly one exists (adoption); webhooks by `name`; profile fields by `name`.
- Ids are **only** read from the observed state or from an op's response, never declared. A reference to an object
  that the same plan creates is resolved at apply time (the plan shows `(known after apply)`).
- Set-valued fields (redirect URIs, scope lists, role scopes, hook events) are compared as sets; ordered ones
  (sign-in methods) as lists. Server defaults are normalized away before diffing so a field Logto fills in does
  not look like drift.
- Ownership is per declared field: an undeclared field of an owned object is left as Logto has it, except where a
  sub-resource is declared as a whole (an organization role's scope set IS the declaration; so are a global role's
  `scopes` when declared, never removing a scope of Logto's Management API).
- Refusals are early: an immutable change (application or role `type`), an ambiguous match, a reference to nothing,
  a resource scope removal that would revoke tokens without `prune`, or a role losing a Management API scope stop
  the plan before any write. A role's declared `scopes` are its exact set: the scopes it loses stay defined on
  their resource, so no `prune` switch is needed.

## 5. Drift, plan and prune

- `plan` prints, per kind, `+ create`, `~ update` (field-level, secrets masked as `(sensitive)`), `- delete` (prune),
  and an unchanged count. `check` = `plan`, exit **0** in sync, **2** drift, **1** error; `apply` exits **3** when it
  completed with warnings.
- A re-apply of an unchanged document issues **zero writes** (tested: the fake counts non-GET calls).
- A sub-object Logto replaces whole (sign-in experience keys, a connector's `config`) is sent as the declared value
  written over what Logto holds (`kindkit.Overlay`): a field no kind manages, set in a console or added by a newer
  Logto, survives. A list is the declared one; its items, when Logto replaces them whole too (sign-in methods),
  are each written over the item Logto holds with the same key.
- Deletes run last, kinds in reverse order. An interrupted apply reports each op: applied, failed, not run; and
  whether the failure is transient (429, 5xx, unreachable) or not.
- A Logto outside the support window is recognised by its own spec (none of the vendored ones, `servers` aside) and
  refused unless `--allow-untested-logto`: its behaviour was never tested, whatever routes its spec lists.
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
proves the route (milestone 5, or dropped: open question 9). Until then, console users are created by hand: the first
visitor of a new console creates its admin account, so that step comes before the console is reachable by anyone
else.

Application-level admins (`platform_admin`-style) are ordinary default-tenant users, out of scope like every user
(section 1). Whether a product grants that right through a global role (declared here, granted by the product or by
hand) or keeps it in its own database is the product's choice.

## 9. Test strategy

1. **Unit, engine on the fake** (`internal/logto/logtofake`): the fake implements `mgmt.ServerInterface`, every
   unmodelled route answers 501; it models observed Logto 1.44.0 behaviours (422 when enabling access
   control without a rule, `page_size` > 100 refused, default page of 20, connector `config` replace-on-PATCH once
   measured). Table tests per kind: create, adopt, update one field, no-op, ambiguous match, prune on/off. Property
   asserted everywhere: **apply twice, the second plan is empty and the fake saw zero writes**.
2. **Contract**: in every kind test the fake validates each request body the kinds really send against every spec
   of the support window, and the fields of each answer it gives (types, declared keys); one problem fails the
   test. Wire types are also validated per version by each kind's contract test.
3. **Behaviour scenarios**, run on the fake and again on a real Logto (4), against the reference configuration
   `examples/reference.yaml` (every kind, committed): first run creates everything; replay changes nothing,
   secrets included; drift repaired without touching secrets (a redirect URI, a hook URL); JWT environment
   variables kept as found; ids only on stdout, secrets only in a 0600 file, never on stdout/stderr; a wrong
   credential fails clearly; two resources with the same audience are refused.
4. **Integration, real Logto 1.44.0** (`test/integration`, build tag): the same scenarios against a fresh Logto, plus
   what only a real Logto can tell (read-back of secrets, PATCH merge vs replace, JWT customizer execution by
   minting and decoding a real token, which endpoints of a "Cloud" spec actually work on OSS). In CI (GitHub-hosted
   runner, section 12b): the same `test/integration/compose.yaml` as locally (Postgres + Logto, the version from
   `LOGTO_VERSION`, with its seeding entrypoint `npm run cli db seed -- --swe && npm run alteration deploy latest &&
   npm start`). No secret involved: the harness reads the throwaway instance's `m-default` secret from its own
   database.
5. **End-to-end, Chainsaw on k3d** (`test/e2e`, `task e2e`): the operator deployed in a throwaway k3d cluster with
   Logto + Postgres in it; scenarios as Chainsaw tests (apply a `LogtoConfig`, assert `status` conditions and the
   published Secret/ConfigMap, edit Logto behind the operator's back and assert `Drifted` then repaired, delete the
   CR and assert Logto untouched). **Done:** pinned k3d 5.9.0, k3s `v1.37.2-k3s1`, chainsaw 0.2.15, kubectl 1.37.2;
   `task e2e` builds the image with ko and imports it (never pulled), deploys Postgres and Logto (`LOGTO_VERSION`,
   default the newest) and the install (`test/e2e/install`, over `config/default`), then runs `test/e2e/operator`:
   those four scenarios, the drift made from inside Logto's Pod with the `m-default` secret on stdin. Run in CI by the
   path-filtered `e2e` workflow on a GitHub-hosted runner.
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
| 7 | Connector `config`: a PATCH with a partial config is refused (the config is validated whole); `GET /api/connectors/{id}` returns `auth.pass` in clear | measured (1.44.0, 1.46.0) | the whole config is always sent; a changed password is drift seen by comparison (Q5 settled) |
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
- **M1 — core kinds through the CLI, on a local Logto (done 2026-11-21).** `api/v1alpha1` types (generated JSON Schema), loader with
  the schema rules of section 3, `internal/logto/client`, kinds: connectors, customProfileFields, signInExperience,
  resources + scopes, organizationScopes, organizationRoles, applications (+ M2M secret, role grants), users
  (application admins, no passwords; removed 2026-11-22, see section 1); `plan` / `apply` / `check` / `render`; `examples/reference.yaml` covering
  those kinds. Acceptance: on an empty local Logto 1.44.0, `apply` of the reference configuration creates every
  declared object (asserted object by object through the API), a second `apply` writes nothing, `check` exits 0
  then 2 after a console edit, `render` shows no secret. Integration test harness (`task it`) run locally and by a
  CI job against a Logto started in the job.
- **Order (decided 2026-11-22).** M3 comes before the rest of M2: the kinds of M1 and global roles already cover a
  first adopter's whole configuration, and what blocks its adoption is publishing the generated ids and secrets
  into the cluster. The remaining M2 kinds land when a document needs them. Before the JWT customizer kind, a probe
  on the throwaway Logto must show that OSS executes the script (a consumer observed it stored and never run).
- **M2 — remaining kinds.** Global roles, access control, webhooks, JWT customizer + id token, account center,
  captcha, `/api/email-templates`; the behaviour scenarios of section 9 complete; outputs files. Before the new
  kinds:
  - a generic "collection by natural key" in `kindkit` (adopt, refuse an ambiguous match, create, update, prune
    with `keep`), and the matching helper in the fake, so that twenty kinds do not each rewrite Logto (done:
    `kindkit.Collection`, used by applications, resources, organization scopes and roles, custom profile fields;
    the fake's `table`, used by organization scopes and roles; connectors keep their own matching, by id or factory). That every write is in `Needs` stays guarded by a test that scans
    each kind's request builders;
  - global roles publish their ids as Refs; applications depend on them and read Refs, so that a role
    created and granted by the same document plans (done: `spec.roles`, `roles.Resolve`; a role the document does
    not declare is still resolved by reading Logto).
- **M3 — operator.** CRD generated from `api/v1alpha1` (controller-gen, committed, drift-guarded), controller-runtime
  manager, Secret watch, resync (`RequeueAfter`, default 10 min, no CronJob), conditions, outputs Secret/ConfigMap,
  `spec.mode: Observe|Apply` (Observe = plan only, status `Drifted`), narrow RBAC, manifests (kustomize, committed,
  the source of the Helm chart of section 12); Chainsaw e2e with Logto inside k3d. From the engine:
  - conditions from each op's `plan.Result`; `engine.Transient` picks a requeue with backoff, anything else
    `Degraded` until the document changes;
  - one CR per Logto endpoint (a second one is refused through an index) and one reconcile at a time per endpoint:
    two documents would fight over the sign-in experience, and one's prune would delete the other's objects;
  - `secretKeyRef` through a `Resolver` that takes a context and reads Kubernetes Secrets; outputs (a flat map) map
    to the keys of one Secret and one ConfigMap;
  - the untested-Logto override becomes a field of the CR.
  Progress: the controller (`internal/operator`, `cmd/logto-operator`) runs the engine per reconcile (`spec.mode`,
  `spec.resyncInterval`, `spec.outputs` with optional key names for `envFrom`, `spec.allowUntestedLogto`), with conditions, outputs, `secretKeyRef`,
  one object per normalized endpoint, Secrets read only for the endpoints their `logto.aliud.fr/readable-by` annotation lists, and a generated
  ClusterRole (docs/operator.md); its manager runs against envtest in `task check`. The install (`config/default`,
  kustomize) and the image (ko, `.ko.yaml`) exist: envtest applies the manifests and runs the manager as their
  ServiceAccount, and `task check` builds the image for every platform. Releases are published by release-please
  (section 12). The Chainsaw e2e runs the install against Logto in k3d (`task e2e`). Next: the Helm chart.
- **M4 — first adopters.** A consuming project renders its `LogtoConfig` and runs `check` in its CI; each
  environment starts in `Observe` (drift reviewed, anything edited by hand in a console shows up), then `Apply`.
- **M5 — admin console users** (only if section 8's spike succeeds).

## 12. Release and versioning

- License: Apache-2.0 (the Kubernetes ecosystem's default; explicit patent grant).
- SemVer tags `vX.Y.Z` (Go modules require SemVer: a deliberate deviation from the studio's CalVer tags).
  `v0.x` until M4. The supported Logto version is part of the release notes; a Logto upgrade = new spec, regenerate,
  minor (or major if the schema breaks) release.
- Releases (decided 2026-11-23): **release-please** (`.github/workflows/release.yml`) keeps a release pull request
  open from the conventional commits; merging it tags `vX.Y.Z`, writes `CHANGELOG.md` and creates the GitHub release.
  `task release` then attaches its artifacts: `install.yaml` (the install, its image built by ko and pinned by
  digest), and the `logtoctl` archives built by GoReleaser (`linux` / `darwin` × `amd64` / `arm64`, CGO off) with
  `checksums.txt`. `logtoctl version` prints the tag.
- Image: built with **ko** (Go-native, reproducible, no Dockerfile), distroless static base, multi-arch. Registry:
  `ghcr.io/<owner>/logto-operator` (public repository, public image, pinned by digest by its consumers). Promotion
  re-tags, never rebuilds. Release workflows publish with the workflow's `GITHUB_TOKEN` (`packages: write`) on tag
  pushes only, never from a pull request.
- Helm chart (decided 2026-11-22): the kustomize manifests are the source; a chart is published next to them as an
  OCI artifact (`oci://ghcr.io/<owner>/charts/logto-operator`), versioned with the release, for the adopters who
  install operators with Helm. Its values cover what an install varies (image, resources, watched namespace,
  resync interval); a test renders the chart with its defaults and compares it with `kustomize build`, so that
  the two never drift apart. The CRD ships in the chart's `crds/` directory.

### 12b. CI runners (decided 2026-11-20)

CI runs on **GitHub-hosted runners** (`ubuntu-<version>`, pinned), which carry a Docker daemon (k3d, the Logto of
`test/integration/compose.yaml`). Never the studio's self-hosted runners: the repository is **public**, and a
fork's pull request would run arbitrary code on studio infrastructure. `task ci:runners` (in `task check`) fails on any other `runs-on`.

The repository started **private** and went public on 2026-11-23. While private, its minutes counted against the
organization's shared free quota, hence: one `check` job running the whole gate, the mise cache, the e2e suite in its
own path-filtered workflow, nothing on draft pull requests, superseded runs cancelled. Hosted minutes are free now:
the gate can be split back into readable jobs.

Rules: no `pull_request_target`; `permissions: contents: read` by default; no secret in any pull request job (the
integration and e2e Logtos are throwaway and local to the job); third-party actions pinned by commit SHA. Blacksmith
was considered (faster, Docker layer cache) and not taken: one more vendor with an org-wide GitHub App, for a CI this
small; switching later is a one-line `runs-on` change.

## 13. Open questions

1. ~~e2e cluster in CI~~ — **decided**: GitHub-hosted runners, k3d in the runner's Docker (12b).
2. ~~Real Logto in CI~~ — **decided**: one `test/integration/compose.yaml` (Postgres + Logto), locally and on the
   runner (9.4, 12b).
3. ~~Names~~ — **decided**: CLI `logtoctl`, CRD group `logto.aliud.fr`, `apiVersion: logto.aliud.fr/v1alpha1`.
4. ~~Environment-specific values~~ — **decided**: `${var}` interpolation, limited to declared `spec.variables`.
5. ~~Write-only secrets drift~~ — **settled 2026-11-21 by measurement**: Logto returns the SMTP password in clear,
   so it is compared like any field (shown as `(sensitive)` in a plan); the whole config is always sent.
6. ~~E-mail templates~~ — **decided**: connector `config.templates` in M1, `/api/email-templates` in M2.
7. **CR deletion.** Leave Logto untouched (my pick, no finalizer) or prune everything the CR declared?
8. **JWT customizer** (gap 6): keep it in M2 behind an integration test that mints a real token and decodes it?
9. **Admin console users** (section 8): spike in M5, or drop the requirement given the `m-admin` blast radius?
10. **Where `LogtoConfig` CRs live**: rendered by each project and bumped into the deployment repository, or
    authored there. It decides who consumes the published ids.
11. **First run per environment in `Observe` mode** (plan only, no write) before switching to `Apply`: make it the
    operator default?
