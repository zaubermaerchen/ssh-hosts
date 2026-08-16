package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/zaubermaerchen/ssh-hosts/internal/finder"
	"github.com/zaubermaerchen/ssh-hosts/internal/sshconfig"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithSelector(args, stdout, stderr, finder.Select)
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
	finderName, err := finder.Normalize(*finderFlag)
	if err != nil {
		fmt.Fprintf(stderr, "ssh-hosts: %v\n", err)
		flags.Usage()
		return 2
	}
	if *useFZF && finderName == "" {
		finderName = "auto"
	}

	configLoader, err := sshconfig.NewLoader()
	if err != nil {
		fmt.Fprintf(stderr, "ssh-hosts: %v\n", err)
		return 1
	}

	configPath := configLoader.DefaultConfigPath()
	if flags.NArg() == 1 {
		configPath = flags.Arg(0)
	}

	hosts, err := configLoader.ListHosts(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "ssh-hosts: %v\n", err)
		return 1
	}
	if finderName != "" {
		code, err := selector(hosts, finderName, stdout, stderr)
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
