// Ponto de entrada do pnn. O alias `procrastina` é o mesmo binário (symlink ou
// cópia) decidido por argv[0] — spec/CLI.md §5/§6.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/onofre-matheus/tcc/cli/internal/app"
	"github.com/onofre-matheus/tcc/cli/internal/cli"
)

func main() {
	a, err := app.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "pnn:", err)
		os.Exit(1)
	}

	args := os.Args[1:]
	switch invokedAs(os.Args[0]) {
	case "procrastina", "procastina", "procrastinar", "procastinar":
		args = append([]string{"procrastina"}, args...)
	}

	root := cli.NewRoot(a)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "pnn:", err)
		os.Exit(1)
	}
}

func invokedAs(argv0 string) string {
	return strings.TrimSuffix(filepath.Base(argv0), ".exe")
}
