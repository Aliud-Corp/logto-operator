# Integration

## External services

- Logto Management API: the only service the tool calls; client in `internal/logto/mgmt`, spec in `api/logto/`.

## Calling conventions

- Token: `client_credentials` with HTTP Basic client auth at `<endpoint>/oidc/token`, resource
  `https://default.logto.app/api`, scope `all` (OSS has no narrower scope). `m-default` gets its token at the console
  endpoint instead.
- Lists default to 20 items and refuse `page_size` above 100: always paginate until a short page (`client.List`).
- `PATCH /api/sign-in-exp` requires the whole `color` object (`primaryColor`, `isDarkModeEnabled`,
  `darkPrimaryColor`) when `color` is sent (read in the spec).
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
- Logto's 400 answers carry `data.issues` (path, message): `client.APIError` keeps them.
- Support window: the latest Logto minor and the two before it (`api/logto/versions.txt`); a new minor is judged
  with `task spec:diff`, which fails only on a breaking change of an operation in `tools/specdiff/used.txt`.
- `/api/swagger.json` (also public at `/api/.well-known/management.openapi.json`) echoes the request host in
  `servers`: `task spec:refresh` always asks `localhost:3001`.
- `m-default` gets its token at the admin endpoint for `https://default.logto.app/api`; the token is accepted by the
  tenant endpoint (measured on 1.44.0, 1.45.0, 1.46.0).
- Never call a deployed Logto from a test or a development session.
