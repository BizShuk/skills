// skills is the entry point for the `skills` CLI.
//
// The actual command tree lives in cmd/root.go (package cmd). This file is
// a thin shell so that `go build .` from the module root produces the binary,
// and so tests / tooling can call cmd.Execute() directly.
package main

import (
	"fmt"
	"os"

	"github.com/bizshuk/gosdk/config"
	"github.com/bizshuk/skills/cmd"
)

func main() {
	config.Default(config.WithAppName("skills"))
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
