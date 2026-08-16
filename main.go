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
	return runWithSelector(args, stdout, stderr, selectWithFZF)
}

type hostSelector func(hosts []string, finder string, stdout, stderr io.Writer) (int, error)

func runWithSelector(args []string, stdout, stderr io.Writer, selector hostSelector) int {
	flags := flag.NewFlagSet("ssh-hosts", flag.ContinueOnError)
	flags.SetOutput(stderr)
	useFZF := flags.Bool("fzf", false, "select one host with the first available fuzzy finder")
	finderFlag := flags.String("finder", "", "fuzzy finder: auto, fzf, sk/skim, or peco (implies --fzf)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: ssh-hosts [--fzf | --finder=NAME] [config-file]")
		fmt.Fprintln(stderr, "List concrete Host names from an OpenSSH client configuration.")
		flags.PrintDefaults()
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
	finder, err := normalizeFinder(*finderFlag)
	if err != nil {
		fmt.Fprintf(stderr, "ssh-hosts: %v\n", err)
		flags.Usage()
		return 2
	}
	if *useFZF && finder == "" {
		finder = "auto"
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
	if finder != "" {
		code, err := selector(hosts, finder, stdout, stderr)
		if err != nil {
			fmt.Fprintf(stderr, "ssh-hosts: %v\n", err)
			return 1
		}
		return code
	}
	for _, host := range hosts {
		fmt.Fprintln(stdout, host)
	}
	return 0
}
