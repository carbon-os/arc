package main

import (
	"fmt"
	"os"

	"github.com/carbon-os/arc/cli"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "build":
		if err := cli.Run(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "\narc build: %v\n", err)
			os.Exit(1)
		}
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "arc: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`arc — build tool for Arc applications

Usage:
  arc build [-o name] [--config arc.json] [go-build-flags] [package]

Commands:
  build     Clone and build libarc, compile your Go module as a shared
            library, and generate a ready-to-build arc-project/ directory.

Flags:
  -o name           Name of the final host binary (default: current directory name)
  --config path     Path to arc.json (default: arc.json in current directory if present)

Any flags not listed above are forwarded to 'go build' unchanged.

Examples:
  arc build .
  arc build -o myapp .
  arc build -o myapp --config arc.json .
  arc build -race -o myapp .

After running arc build:
  cd arc-project && cmake --build build
  — or open arc-project/build/*.xcodeproj in Xcode for a native debug session
`)
}