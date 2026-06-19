package main

import (
	"fmt"
	"os"

	"github.com/jinmu/eme/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "eme: %v\n", err)
		os.Exit(1)
	}
}
