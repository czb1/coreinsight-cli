package main

import (
	"os"

	"coreinsight-cli/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
