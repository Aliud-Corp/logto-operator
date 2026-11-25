# Using logtoctl

`logtoctl` reconciles a self-hosted Logto with a `LogtoConfig` document, through Logto's Management API only. It
never writes to Logto's database. [examples/reference.yaml](../examples/reference.yaml) declares every kind
supported today.

## Supported Logto versions

The latest Logto OSS minor and the two before it: **1.44, 1.45 and 1.46** today
([api/logto/versions.txt](../api/logto/versions.txt)). Every kind is tested against a real Logto of each of them. A
field the document declares that the target Logto does not accept is refused at plan time, before any write: the
target's own API spec decides.

`logtoctl` recognises the version from the API spec the target serves. A Logto whose spec is none of the supported
ones (an older or newer minor, a patched build) is refused, because its behaviour was never tested; pass
`--allow-untested-logto` to run anyway, at your own risk.

## Install and build

Each [release](https://github.com/Aliud-Corp/logto-operator/releases) carries a static binary per platform,
`logtoctl_<version>_<os>_<arch>.tar.gz` (linux and darwin, amd64 and arm64), and `checksums.txt`. With mise:

```bash
mise use github:Aliud-Corp/logto-operator@<version>
```

From a checkout:

```bash
mise install      # the pinned toolchain
task build        # writes bin/logtoctl, a static binary
```

## The document

```yaml
apiVersion: logto.aliud.fr/v1alpha1
kind: LogtoConfig
metadata: {name: example}
spec:
  endpoint: https://auth.example.test          # where the Management API answers
  credentials:
    clientId: {valueFrom: {env: LOGTO_CLIENT_ID}}
    clientSecret: {valueFrom: {env: LOGTO_CLIENT_SECRET}}
    tokenEndpoint: https://console.example.test # only for the m-default route (see below)
  variables: {zone: example.test}              # usable as ${zone} in string fields
  # connectors, customProfileFields, signInExperience, resources, organizationScopes,
  # organizationRoles, roles, applications: see examples/reference.yaml
```

- **Strict.** An unknown key fails with its path, and `${name}` only reads `spec.variables`. Every rule is checked
  before any call: the document's JSON Schema (`api/v1alpha1/logtoconfig.schema.json`, the same rules as the CRD:
  required fields, URLs, colours, enums), then unique names, references that resolve, SMTP templates, distinct
  audiences, and so on.
- **Secrets are never written in the document.** They are read from `valueFrom.env` or `valueFrom.file` at run time.
- **Files.** `fromFile` and `fromDir` are resolved relative to the document, except the `content.fromFile` of a
  template read by `fromDir`, which is relative to that template's file (several documents can share one directory
  of templates). Use `as: dataUri` for images.
- **One document.** A file holds a single `LogtoConfig`; a stream of several documents is refused.

## Credentials: the first M2M application

The tool authenticates as a machine-to-machine application that holds Logto's `Logto Management API access` role.
Logto cannot create that first application through its API, so it is a one-time step per Logto instance:

- **Dedicated application** (recommended). In the Logto console, create a machine-to-machine application and give
  it the `Logto Management API access` role. Its id and secret become `LOGTO_CLIENT_ID` and `LOGTO_CLIENT_SECRET`.
  Leave `tokenEndpoint` unset.
- **`m-default` route.** Logto's built-in admin-tenant application `m-default` can call the default tenant's
  Management API. Its secret is in Logto's database
  (`select secret from applications where id = 'm-default'`), read once. It gets its token at the console
  endpoint: set `tokenEndpoint` to it.

## Commands

| Command | Does | Exit codes |
| --- | --- | --- |
| `logtoctl render -f FILE` | prints the document as it will be applied (files inlined, variables applied and dropped, a literal `${` written `$${`): what an operator object is made of | 0, 1 |
| `logtoctl plan -f FILE` | prints what `apply` would do; writes nothing | 0, 1 |
| `logtoctl check -f FILE` | like `plan`, and exits 2 when Logto differs from the document | 0, 1, 2 |
| `logtoctl apply -f FILE` | reconciles; prints the plan it ran on stderr and the identifiers on stdout | 0, 1, 3 |

`-f -` reads the document on stdin; its `fromFile` and `fromDir` are then relative to the working directory. A
document assembled by kustomize (a base and one overlay per environment) is rendered, and checked, in one line:

```bash
kubectl kustomize overlays/staging | logtoctl render -f - > rendered/staging/logto.yaml
```

An `apply` publishes the identifiers Logto generates (`applications.<key>.id`, `resources.<key>.indicator`)
on stdout, as `name=value` lines or, with `--outputs json`, as JSON. The secrets it publishes (an
M2M application with `secret: {publish: true}`) are never printed; they go to:

- `--secrets-out FILE`: JSON, created with mode 0600, also forced on an existing file;
- `--secrets-fd N`: JSON written to an inherited descriptor, e.g. `--secrets-fd 3 3>secrets.json`;
- stdout, only with `--print-secrets`, which you should avoid.

An apply that publishes secrets without writing them anywhere exits 3.

An apply that stops on an error says what it did: how many changes were applied, which one failed and how many
were never run. Nothing is rolled back: run `apply` again once the cause is fixed, it starts from what Logto holds.
When the failure is transient (Logto overloaded, failing or unreachable), the message says a later apply may
succeed.

## Third-party applications

An application declared with `isThirdParty: true` (Native, SPA or Traditional) shows its users a consent screen, and
its tokens are capped to the scopes that screen may grant. `consentScopes` lists them: `resources` are scopes of the
resources declared in `spec.resources`, `user` are Logto's user scopes (`profile`, `email`, ...;
`urn:logto:scope:personal_access_tokens` needs Logto 1.46). Organization scopes are not managed yet.

```yaml
  applications:
    connector:
      name: Example Connector
      type: Traditional
      isThirdParty: true
      consentScopes:
        resources: [{resource: app, scope: "event:read"}]
        user: [profile, email]
```

`isThirdParty` is create-only in Logto: an existing application with another value is refused at plan time.
`consentScopes` on an application that is not third-party is refused by the document rules.

## What the tool never does

- It never deletes an object that the document does not declare, unless that kind's `spec.prune` switch is set.
  Even then it never prunes Logto's own Management API resource, a global role holding one of its scopes,
  a role the document grants, or what `spec.prune.keep` lists (by name; a resource also by indicator, a connector by id).
- It never recreates an application: applications are matched by name, and their type and `isThirdParty` cannot
  change (Logto sets both at creation: the plan refuses a difference).
- It manages an application's consent scopes only when `consentScopes` lists are declared; a declared list is exact
  (the application holds those scopes and no other of its kind), an omitted one leaves what Logto holds.
- It never rotates a secret, and never sends a user a password.
- It never revokes a role an application or a user holds; it only grants the declared ones. A role granted by
  name is either declared in `spec.roles` (created in the same run if needed) or must already exist in Logto.
- It never changes a global role's type. A role's scopes are managed only when its `scopes` are declared; then it
  holds exactly those, except that a scope of Logto's Management API is never taken away (the plan refuses).
- It never drops what Logto holds next to a declared value: when it must send a whole object (a connector's
  config, a part of the sign-in experience), the fields it does not manage are sent back as Logto has them.
