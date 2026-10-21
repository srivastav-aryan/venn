// Command venn runs a single local command through the runner package.
package main

import (
	"context"
	"fmt"
	"os"

	"venn/internal/runner"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	if len(argv) == 0 || argv[0] == "" {
		fmt.Fprintln(os.Stderr, "usage: venn <program> [arguments...]")
		return 2
	}

	result, err := runner.Run(context.Background(), runner.Action{Argv: argv})
	if len(result.Stdout) > 0 {
		_, _ = os.Stdout.Write(result.Stdout)
	}
	if len(result.Stderr) > 0 {
		_, _ = os.Stderr.Write(result.Stderr)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return result.ExitCode
}
