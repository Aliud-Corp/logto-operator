// Command logtoctl applies a Logto configuration declared as code through Logto's Management API.
//
// SKELETON: only `version` exists. plan / apply / check / render arrive with milestone 1 (docs/plan.md).
package main

import (
	"fmt"
	"os"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println(version)
		return
	}
	fmt.Fprintln(os.Stderr, "usage: logtoctl version   (plan, apply, check, render: not implemented yet, see docs/plan.md)")
	os.Exit(2)
}
