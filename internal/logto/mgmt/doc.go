// Package mgmt is the typed client of Logto's Management API, GENERATED from the vendored OpenAPI document
// api/logto/management.openapi.json (Logto OSS 1.44.0). Never edit a *.gen.go file: change the spec or the pinned
// generator and run `task generate`. `task generate:check` (CI, every push) fails when the committed code differs
// from a fresh generation.
//
// Three files, one per oapi-codegen config next to them:
//
//   - types.gen.go   request and response types (models)
//   - client.gen.go  Client and ClientWithResponses, what the engine calls
//   - server.gen.go  the chi ServerInterface and its Unimplemented base, used ONLY by test fakes of the Management
//     API: every route a fake does not model answers 501, so a test can never pass against a route nobody wrote.
//
// Updating Logto = replace the spec (api/logto/README.md), `task generate`, fix what no longer compiles.
package mgmt
