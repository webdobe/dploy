// Command dploy is the entrypoint for the dploy CLI.
//
// It is intentionally thin: all command logic lives in internal/cli.
// This file's only other job is mapping CLI errors to exit codes.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/webdobe/dploy/internal/cli"
	"github.com/webdobe/dploy/internal/failure"
)

func main() {
	err := cli.Execute()
	if err == nil {
		return
	}

	fmt.Fprintln(os.Stderr, err)

	var ec *failure.ExitCodeError
	if errors.As(err, &ec) {
		os.Exit(ec.Code)
	}
	os.Exit(failure.ExitGeneralFailure)
}
