# Integration

## External services

- Logto Management API: the only service the tool calls; client in `internal/logto/mgmt`, spec in `api/logto/`.

## Calling conventions

- Token: `client_credentials` with HTTP Basic client auth at `<endpoint>/oidc/token`, resource
  `https://default.logto.app/api`, scope `all` (OSS has no narrower scope). `m-default` gets its token at the console
  endpoint instead.
- Lists default to 20 items and refuse `page_size` above 100: always paginate until a short page.
- Logto generates application ids, organization ids and M2M secret values; none can be chosen through the API.
- An e-mail sign-up identifier with `verify: true` is refused while no e-mail connector exists.
- Enabling an application's access control without a rule answers 422.
- SMTP connector config: `port` must be a number, `auth` must be non-empty, and `Register`, `SignIn` and
  `ForgotPassword` templates are mandatory.
- Never call a deployed Logto from a test or a development session.
