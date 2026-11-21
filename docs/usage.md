# Using logtoctl

`logtoctl` reconciles a self-hosted Logto with a `LogtoConfig` document, through Logto's Management API only. It
never writes to Logto's database. [examples/reference.yaml](../examples/reference.yaml) declares every kind
supported today.

## Supported Logto versions

The latest Logto OSS minor and the two before it: **1.44, 1.45 and 1.46** today
([api/logto/versions.txt](../api/logto/versions.txt)). Every kind is tested against a real Logto of each of them. A
field the document declares that the target Logto does not accept is refused at plan time, before any write: the
target's own API spec decides.

## Install and build

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
  # organizationRoles, applications, users: see examples/reference.yaml
```

- **Strict.** An unknown key fails with its path, and `${name}` only reads `spec.variables`. Every rule is checked
  before any call: unique names, references that resolve, SMTP templates, distinct audiences, and so on.
- **Secrets are never written in the document.** They are read from `valueFrom.env` or `valueFrom.file` at run time.
- **Files.** `fromFile` and `fromDir` are resolved relative to the document. Use `as: dataUri` for images.

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
| `logtoctl render -f FILE` | prints the document as it will be applied (files inlined, variables replaced) | 0, 1 |
| `logtoctl plan -f FILE` | prints what `apply` would do; writes nothing | 0, 1 |
| `logtoctl check -f FILE` | like `plan`, and exits 2 when Logto differs from the document | 0, 1, 2 |
| `logtoctl apply -f FILE` | reconciles; prints the plan it ran on stderr and the identifiers on stdout | 0, 1, 3 |

An `apply` publishes the identifiers Logto generates (`applications.<key>.id`, `resources.<key>.indicator`,
`users.<key>.id`) on stdout, as `name=value` lines or, with `--outputs json`, as JSON. The secrets it publishes (an
M2M application with `secret: {publish: true}`) are never printed; they go to:

- `--secrets-out FILE`: JSON, created with mode 0600, also forced on an existing file;
- `--secrets-fd N`: JSON written to an inherited descriptor, e.g. `--secrets-fd 3 3>secrets.json`;
- stdout, only with `--print-secrets`, which you should avoid.

An apply that publishes secrets without writing them anywhere exits 3.

## What the tool never does

- It never deletes an object that the document does not declare, unless that kind's `spec.prune` switch is set.
  Even then it never prunes users, Logto's own Management API resource, or names listed in `spec.prune.keep`.
- It never recreates an application: applications are matched by name and their type cannot change.
- It never rotates a secret, and never sends a user a password.
- It never revokes a role an application or a user holds; it only grants the declared ones.
