# Running the operator

The operator reconciles `LogtoConfig` objects on the same engine as `logtoctl`: the document is the one described in
[usage.md](usage.md), applied as a Kubernetes object. It is milestone 3 of [plan.md](plan.md), in progress: the
controller runs; manifests, image and end-to-end tests come next.

## What it does on each reconcile

- It turns the object into a document like `logtoctl` does a file (variables, strict keys, the JSON Schema, the rules
  across fields). It reads no file: `fromFile` and `fromDir` are refused, so render the document first
  (`logtoctl render -f config.yaml`: files inlined, variables applied and dropped, a literal `${` written `$${`)
  and apply the result.
- It reads `secretKeyRef` values only, from Secrets of the object's namespace, uncached, all of them before its
  first call to Logto. A Secret is read only for the endpoints its annotation lists:

  ```yaml
  metadata:
    annotations:
      logto.aliud.fr/readable-by: https://auth.example.test   # comma-separated; the token endpoint too, if any
  ```

  Endpoints are compared like the endpoint index (below). Without it, whoever may create a LogtoConfig in the
  namespace could send the Secret, the Logto credentials included, to an endpoint of their choice. `env` and `file`
  sources are refused: the operator never reads its own environment or files.
- `spec.mode: Observe` (the default) plans and reports what differs; `spec.mode: Apply` reconciles Logto.
- It publishes the identifiers Logto generates in the ConfigMap `spec.outputs.configMap.name`, and, after an apply,
  the secrets in the Secret `spec.outputs.secret.name`. Both are owned by the object and labelled
  `app.kubernetes.io/managed-by: logto-operator`.
- It comes back every `spec.resyncInterval` (default `10m`, at least `30s`), at once when the object's spec, labels
  or annotations change (not its status), when someone changes or deletes an output it owns, and when another object
  of its endpoint goes away or moves. A changed credential Secret is read at the next resync.
- An object being deleted is left alone: no write to Logto, no output re-created while the garbage collector removes
  them.

## Outputs

Every output has a name: `applications.<key>.id`, `applications.<key>.secret` (an M2M application with
`secret: {publish: true}`), `resources.<key>.indicator`, where `<key>` is the document's local key.
Without `keys`, a target publishes every output under its name; read them one by one:

```yaml
env:
  - name: LOGTO_APP_ID
    valueFrom: {configMapKeyRef: {name: logto-ids, key: applications.organizer.id}}
```

With `keys`, a target publishes only the outputs it lists, under the names it gives, so a Pod takes it whole:

```yaml
spec:
  outputs:
    configMap:
      name: logto-ids
      keys: {LOGTO_APP_ID: applications.organizer.id, LOGTO_API_RESOURCE: resources.app.indicator}
    secret:
      name: logto-secrets
      keys: {LOGTO_MANAGEMENT_CLIENT_SECRET: applications.backend.secret}
---
# in the Deployment
envFrom:
  - configMapRef: {name: logto-ids}
  - secretRef: {name: logto-secrets}
```

The document is refused when a key names no output, puts a secret in the ConfigMap or an identifier in the Secret,
appears in both targets (with or without `keys`), or is no valid ConfigMap key (nor is an output name without `keys`). A target holds exactly
what the document publishes now. An outputs name taken by an object the LogtoConfig does not own is never
overwritten: the object is `Degraded` (reason `OutputsConflict`) until the name changes. In Observe mode the Secret is not written and the ConfigMap holds the identifiers of
the objects that already exist.

## Status

| Condition | True when |
| --------- | --------- |
| `Ready` | the last reconcile succeeded and Logto matches the document |
| `Drifted` | Logto differs from the document (Observe reports it, Apply removes it) |
| `Degraded` | the document or the target is in error (invalid document, missing Secret, untested Logto, failed write); it stays until the object changes or the next resync succeeds |

A transient failure (Logto unreachable, 429, 5xx) leaves `Ready` false with reason `Transient` and is retried with
backoff, the API server's own transient answers included; it leaves `Degraded` as the last full run found it. A run
that did not plan to its end leaves `Drifted` Unknown. `Degraded` carries a reason: `InvalidDocument`,
`SecretNotReadable`, `EndpointTaken`, `TargetRefused`, `UntestedLogto`, `ApplyFailed`, `ReconcileFailed`,
`OutputsConflict`. `status.observedGeneration` is the generation the last run read, whatever came of it (tools that
compare it with `metadata.generation` see the failure, not a run in progress). `status.logtoVersion` is the
supported version the target matched, empty when untested; `status.lastAppliedTime` is when the last apply that
wrote ended, even one that failed after writing.

## One object per Logto

Two objects on one endpoint would fight over the sign-in experience and prune each other's objects: the oldest one
keeps the endpoint, any other is `Degraded` (reason `EndpointTaken`) and writes nothing, until the first goes away.
Endpoints are compared without scheme, host case, default port and trailing slashes; one Logto reached through two
hosts (a Service and an ingress) cannot be told apart, so name it one way. Several objects reconcile at once
(`--max-concurrent-reconciles`, default 4); runs on one endpoint take turns, and an object that does not have the
endpoint learns it without waiting for that turn.

## Running

Flags: `--leader-elect` (default true: two active replicas would both write to Logto), `--leader-election-namespace`
(where the lease lives; the Pod's namespace in a cluster), `--metrics-bind-address` (default `0`, off; plain HTTP
when set, keep it inside the cluster), `--health-probe-bind-address` (default `:8081`, `/healthz` and `/readyz`),
`--max-concurrent-reconciles`.

Outside a cluster (a developer's kubeconfig), there is no Pod namespace: pass `--leader-elect=false`, or
`--leader-election-namespace`.

## Permissions

The ClusterRole (`config/rbac/role.yaml`, generated) reads `LogtoConfig` objects and writes their status and
finalizers (owner references with `blockOwnerDeletion`), reads and writes Secrets and ConfigMaps, and manages the
leader-election lease and its events. The operator's cache holds only the Secrets and ConfigMaps it labels; the
credentials a document names are read one by one. Listing and watching Secrets stays cluster-wide: RBAC cannot
filter by label.
