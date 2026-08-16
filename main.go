package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ssh-hosts", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: ssh-hosts [config-file]")
		fmt.Fprintln(stderr, "List concrete Host names from an OpenSSH client configuration.")
	}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() > 1 {
		fmt.Fprintln(stderr, "ssh-hosts: expected at most one config file")
		flags.Usage()
		return 2
	}

	ctx, err := systemExpansionContext()
	if err != nil {
		fmt.Fprintf(stderr, "ssh-hosts: %v\n", err)
		return 1
	}

	configPath := filepath.Join(ctx.homeDir, ".ssh", "config")
	if flags.NArg() == 1 {
		configPath = flags.Arg(0)
	}

	hosts, err := newHostLoader(ctx).load(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "ssh-hosts: %v\n", err)
		return 1
	}
	for _, host := range hosts {
		fmt.Fprintln(stdout, host)
	}
	return 0
}
