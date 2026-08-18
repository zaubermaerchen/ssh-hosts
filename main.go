package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/zaubermaerchen/ssh-hosts/internal/finder"
	"github.com/zaubermaerchen/ssh-hosts/internal/sshconfig"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithSelector(args, stdout, stderr, finder.Select)
}

type hostSelector func(items []finder.Item, finder string, stdout, stderr io.Writer) (int, error)

type jsonHost struct {
	Alias       string `json:"alias"`
	User        string `json:"user"`
	Hostname    string `json:"hostname"`
	Port        int    `json:"port"`
	Destination string `json:"destination"`
}

func runWithSelector(args []string, stdout, stderr io.Writer, selector hostSelector) int {
	flags := flag.NewFlagSet("ssh-hosts", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "output hosts as a JSON array")
	detailsOutput := flags.Bool("details", false, "output hosts with resolved connection destinations")
	useFZF := flags.Bool("fzf", false, "select one host with the first available fuzzy finder")
	finderFlag := flags.String("finder", "", "fuzzy finder: auto, fzf, sk/skim, or peco (implies --fzf)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: ssh-hosts [--details] [--fzf] [--finder=NAME] [config-file]")
		fmt.Fprintln(stderr, "       ssh-hosts --json [config-file]")
		fmt.Fprintln(stderr, "List SSH aliases, optionally with resolved connection destinations.")
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
	if *jsonOutput && finderName != "" {
		fmt.Fprintln(stderr, "ssh-hosts: --json cannot be combined with --fzf or --finder")
		flags.Usage()
		return 2
	}
	if *detailsOutput && *jsonOutput {
		fmt.Fprintln(stderr, "ssh-hosts: --details cannot be combined with --json")
		flags.Usage()
		return 2
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
	if *jsonOutput {
		if err := writeJSONHosts(stdout, hosts); err != nil {
			fmt.Fprintf(stderr, "ssh-hosts: encode JSON: %v\n", err)
			return 1
		}
		return 0
	}
	items := make([]finder.Item, 0, len(hosts))
	for _, host := range hosts {
		display := host.Alias + "\t" + host.Destination()
		value := host.Alias
		if *detailsOutput {
			value = display
		}
		items = append(items, finder.Item{
			Value:   value,
			Display: display,
		})
	}
	if finderName != "" {
		code, err := selector(items, finderName, stdout, stderr)
		if err != nil {
			fmt.Fprintf(stderr, "ssh-hosts: %v\n", err)
			return 1
		}
		return code
	}
	for _, item := range items {
		value := item.Value
		if *detailsOutput {
			value = item.Display
		}
		if _, err := fmt.Fprintln(stdout, value); err != nil {
			fmt.Fprintf(stderr, "ssh-hosts: write output: %v\n", err)
			return 1
		}
	}
	return 0
}

func writeJSONHosts(output io.Writer, hosts []sshconfig.Host) error {
	jsonHosts := make([]jsonHost, 0, len(hosts))
	for _, host := range hosts {
		port, err := strconv.Atoi(host.Port)
		if err != nil {
			return fmt.Errorf("invalid port %q for host %q: %w", host.Port, host.Alias, err)
		}
		jsonHosts = append(jsonHosts, jsonHost{
			Alias:       host.Alias,
			User:        host.User,
			Hostname:    host.HostName,
			Port:        port,
			Destination: host.Destination(),
		})
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(jsonHosts)
}
