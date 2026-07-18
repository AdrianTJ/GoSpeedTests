// Command loadstar is the single entry point for Loadstar: `loadstar run`
// performs a one-off test from the command line, `loadstar serve` starts the
// API daemon with the status page.
package main

import (
	"fmt"
	"os"
)

const usage = `Loadstar — self-hosted web performance testing and monitoring.

Usage:
  loadstar run -u <url> [options]   Run a one-off test from the command line
  loadstar serve [options]          Start the API daemon (REST API + status page)

Run "loadstar <command> -h" for the options of each command.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		runCmd(os.Args[2:])
	case "serve":
		serveCmd(os.Args[2:])
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}
