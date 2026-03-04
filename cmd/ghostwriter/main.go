package main

import (
	"os"

	"github.com/ianmclaughlin/ghostwriter/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
