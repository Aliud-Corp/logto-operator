# Integration

## External services

- Logto Management API: the only service the tool calls; client in `internal/logto/mgmt`, spec in `api/logto/`.

## Calling conventions

- Token: `client_credentials` with HTTP Basic client auth at `<endpoint>/oidc/token`, resource
  `https://default.logto.app/api`, scope `all` (OSS has no narrower scope). `m-default` gets its token at the console
  endpoint instead.
- Lists default to 20 items and refuse `page_size` above 100: always paginate until a short page (`client.List`).
- `PATCH /api/sign-in-exp` merges top-level keys but needs `color`, `languageInfo`, `signUp` and each sign-in
  method whole (read in the spec): the kind fills an undeclared part from what Logto holds.
- An e-mail sign-up identifier with `verify` needs an e-mail connector: the plan refuses it before Logto does.
- Logto generates application ids, organization ids and M2M secret values; none can be chosen through the API.
- An e-mail sign-up identifier with `verify: true` is refused while no e-mail connector exists.
- Enabling an application's access control without a rule answers 422.
- Custom profile fields: the body is one schema per type, `required` is mandatory (even `false`); Logto may add
  defaults to `config`, so a field's config is compared as "what the document declares is there".
- Prune in integration tests touches only the test's own objects (`kindtest.KeepAllBut`); integration packages run
  one at a time (`go test -p 1`) against the shared throwaway Logto.
- SMTP connector config: `port` must be a number, `auth` must be non-empty, and `Register`, `SignIn`,
  `ForgotPassword` and `Generic` templates are mandatory (measured: without `Generic`, 400 `connector.invalid_config`).
- SMTP connector: `GET /api/connectors/{id}` returns `auth.pass` in clear, and a PATCH with a partial config is
  refused: always send the whole config (asserted by `test/integration/connector_probe_test.go`).
- The client replays a 429 always, a 5xx only for GET, HEAD, PUT and DELETE: a POST may have been written already.
- Logto's 400 answers carry `data.issues` (path, message): `client.APIError` keeps them.
- Support window: the latest Logto minor and the two before it (`api/logto/versions.txt`); a new minor is judged
  with `task spec:diff`, which fails only on a breaking change of an operation in `tools/specdiff/used.txt`.
- `/api/swagger.json` (also public at `/api/.well-known/management.openapi.json`) echoes the request host in
  `servers`: `task spec:refresh` always asks `localhost:3001`.
- `m-default` gets its token at the admin endpoint for `https://default.logto.app/api`; the token is accepted by the
  tenant endpoint (measured on 1.44.0, 1.45.0, 1.46.0).
- Never call a deployed Logto from a test or a development session.
- Global roles: `POST /api/roles` answers 200 (not 201), `description` is mandatory and `type` cannot change;
  `POST /api/roles/{id}/scopes` adds scopes and answers 201 with the role's scopes, without their resource.
- Logto's "Logto Management API access" role holds the `all` scope of `https://default.logto.app/api`: prune never
  deletes a role holding a scope of that resource.
- `isThirdParty` is a field of `POST /api/applications` only (absent from the update body): create-only, like `type`;
  the plan refuses a difference. Consent scopes (`/api/applications/{id}/user-consent-scopes`) exist for third-party
  applications only; `POST` adds, `DELETE .../{scopeType}/{scopeId}` removes (the scope id of a resource scope, the
  name of a user scope), and the user scope enum gains `urn:logto:scope:personal_access_tokens` in 1.46.0.
- CIMD (1.44+, same shapes in 1.44 to 1.46): `GET`/`PATCH /api/configs/cimd` (`enabled`, experimental
  `addConsentPromptForOfflineAccess`, which an answer may omit) and tenant-wide consent scopes on
  `/api/cimd/user-consent-scopes` (list, `POST` adds, `DELETE .../{scopeType}/{scopeId}`), exactly the application
  routes minus the application (and without its third-party requirement). `PATCH` answers 422 `request.invalid_input`
  when Logto's SSRF protection is disabled or has an allowlist (measured with `SSRF_PROTECTION_DISABLED=true`); the
  throwaway Logto of the integration tests accepts it. The `consentscopes` package is the diff and apply both kinds
  share.
