package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/zaubermaerchen/ssh-hosts/internal/finder"
	"github.com/zaubermaerchen/ssh-hosts/internal/sshconfig"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithSelector(args, stdout, stderr, finder.Select)
}

type hostSelector func(items []finder.Item, stdout, stderr io.Writer) (int, error)

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
	selectOutput := flags.Bool("select", false, "select one host interactively")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: ssh-hosts [--details] [--select] [config-file]")
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
	if *jsonOutput && *selectOutput {
		fmt.Fprintln(stderr, "ssh-hosts: --json cannot be combined with --select")
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
	maxAliasWidth := maxHostAliasWidth(hosts)
	items := make([]finder.Item, 0, len(hosts))
	for _, host := range hosts {
		details := host.Alias + "\t" + host.Destination()
		display := formatFinderDisplay(host, maxAliasWidth)
		value := host.Alias
		if *detailsOutput {
			value = details
		}
		items = append(items, finder.Item{
			Value:   value,
			Display: display,
		})
	}
	if *selectOutput {
		code, err := selector(items, stdout, stderr)
		if err != nil {
			fmt.Fprintf(stderr, "ssh-hosts: %v\n", err)
			return 1
		}
		return code
	}
	for _, item := range items {
		if _, err := fmt.Fprintln(stdout, item.Value); err != nil {
			fmt.Fprintf(stderr, "ssh-hosts: write output: %v\n", err)
			return 1
		}
	}
	return 0
}

func maxHostAliasWidth(hosts []sshconfig.Host) int {
	maxWidth := 0
	for _, host := range hosts {
		if width := finderDisplayWidth(host.Alias); width > maxWidth {
			maxWidth = width
		}
	}
	return maxWidth
}

func formatFinderDisplay(host sshconfig.Host, maxAliasWidth int) string {
	padding := maxAliasWidth - finderDisplayWidth(host.Alias) + 2
	if padding < 2 {
		padding = 2
	}
	return host.Alias + strings.Repeat(" ", padding) + host.Destination()
}

func finderDisplayWidth(value string) int {
	width := 0
	for _, r := range value {
		width += runewidth.RuneWidth(r)
	}
	return width
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
