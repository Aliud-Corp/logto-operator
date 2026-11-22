# Running the operator

The operator reconciles `LogtoConfig` objects on the same engine as `logtoctl`: the document is the one described in
[usage.md](usage.md), applied as a Kubernetes object. It is milestone 3 of [plan.md](plan.md), in progress: the
controller runs; manifests, image and end-to-end tests come next.

## What it does on each reconcile

- It turns the object into a document like `logtoctl` does a file (variables, strict keys, the JSON Schema, the rules
  across fields). It reads no file: `fromFile` and `fromDir` are refused, so render the document first
  (`logtoctl render -f config.yaml`, files inlined) and apply the result.
- It reads `secretKeyRef` values from Secrets of the object's namespace, uncached, at each run.
- `spec.mode: Observe` (the default) plans and reports what differs; `spec.mode: Apply` reconciles Logto.
- It publishes the identifiers Logto generates in the ConfigMap `spec.outputs.configMap.name`, and, after an apply,
  the secrets in the Secret `spec.outputs.secret.name`. Both are owned by the object and labelled
  `app.kubernetes.io/managed-by: logto-operator`.
- It comes back every `spec.resyncInterval` (default `10m`), and at once when the object changes. A changed
  credential Secret is read at the next resync.

## Outputs

Every output has a name: `applications.<key>.id`, `applications.<key>.secret` (an M2M application with
`secret: {publish: true}`), `resources.<key>.indicator`, `users.<key>.id`, where `<key>` is the document's local key.
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
or is no valid ConfigMap key. In Observe mode the Secret is not written and the ConfigMap holds the identifiers of
the objects that already exist.

## Status

| Condition | True when |
| --------- | --------- |
| `Ready` | the last reconcile succeeded and Logto matches the document |
| `Drifted` | Logto differs from the document (Observe reports it, Apply removes it) |
| `Degraded` | the document or the target is in error (invalid document, missing Secret, untested Logto, failed write); it stays until the object changes or the next resync succeeds |

A transient failure (Logto unreachable, 429, 5xx) leaves `Ready` false with reason `Transient` and is retried with
backoff. `status.logtoVersion` is the supported version the target matched; `status.lastAppliedTime` is the last
apply that wrote.

## One object per Logto

Two objects on one endpoint would fight over the sign-in experience and prune each other's objects: the oldest one
keeps the endpoint, any other is `Degraded` and writes nothing. Reconciles run one at a time.

## Permissions

The ClusterRole (`config/rbac/role.yaml`, generated) reads `LogtoConfig` objects and writes their status, and
reads and writes Secrets and ConfigMaps. The operator's cache holds only the Secrets and ConfigMaps it labels; the
credentials a document names are read one by one.
