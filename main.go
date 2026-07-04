// skills is the entry point for the `skills` CLI.
//
// The actual command tree lives in cmd/skills/root.go (package cmd). This
// file is a thin shell so that `go build .` from the module root produces
// the binary, and so tests / tooling can call cmd.Execute() directly.
package main

import (
	"fmt"
	"os"

	"github.com/bizshuk/skills/cmd/skills"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}