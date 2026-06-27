package main

import (
	"fmt"
	"os"

	"github.com/alderwork/eme/internal/cmd"
	"github.com/alderwork/eme/internal/errors"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "eme: %v\n", err)
		// A distinct exit code for the unpushed-history refusal lets the dashboard
		// recognize it and offer --force-unpushed in-UI rather than parsing stderr.
		os.Exit(errors.ExitCode(err))
	}
}
