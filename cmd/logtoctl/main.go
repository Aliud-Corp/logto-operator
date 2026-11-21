// Command logtoctl applies a Logto configuration declared as code through Logto's Management API
// (internal/cli documents the commands, docs/usage.md how to run them).
package main

import "github.com/Aliud-Corp/logto-operator/internal/cli"

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() { cli.Main(version) }
