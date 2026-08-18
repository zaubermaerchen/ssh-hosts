package finder

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type finderSpec struct {
	name       string
	executable string
	arguments  []string
}

// Item separates the text shown in the finder from the value returned after
// selection.
type Item struct {
	Value   string
	Display string
}

var supportedFinders = []finderSpec{
	{name: "fzf", executable: "fzf", arguments: []string{"--prompt=SSH host> ", "--no-multi"}},
	{name: "sk", executable: "sk", arguments: []string{"--prompt=SSH host> ", "--no-multi"}},
	{
		name:       "peco",
		executable: "peco",
		arguments:  []string{"--prompt=SSH host> ", "--initial-filter=Fuzzy", "--on-cancel=error"},
	},
}

// Normalize validates a finder name and converts aliases to executable names.
func Normalize(name string) (string, error) {
	switch strings.ToLower(name) {
	case "":
		return "", nil
	case "auto", "fzf", "sk", "peco":
		return strings.ToLower(name), nil
	case "skim":
		return "sk", nil
	default:
		return "", fmt.Errorf("unsupported fuzzy finder %q (use auto, fzf, sk/skim, or peco)", name)
	}
}

func resolveFinder(name string) (finderSpec, error) {
	return resolveFinderWith(name, exec.LookPath)
}

func resolveFinderWith(name string, lookPath func(string) (string, error)) (finderSpec, error) {
	if name == "auto" {
		for _, finder := range supportedFinders {
			if _, err := lookPath(finder.executable); err == nil {
				return finder, nil
			}
		}
		return finderSpec{}, fmt.Errorf("no supported fuzzy finder found in PATH (tried fzf, sk, peco)")
	}
	for _, finder := range supportedFinders {
		if finder.name != name {
			continue
		}
		if _, err := lookPath(finder.executable); err != nil {
			return finderSpec{}, fmt.Errorf("fuzzy finder %q not found in PATH", finder.executable)
		}
		return finder, nil
	}
	return finderSpec{}, fmt.Errorf("unsupported fuzzy finder %q", name)
}

// Select runs a fuzzy finder with items as input and writes the selected
// item's value.
func Select(items []Item, finderName string, stdout, stderr io.Writer) (int, error) {
	if len(items) == 0 {
		return 1, nil
	}
	finder, err := resolveFinder(finderName)
	if err != nil {
		return 1, err
	}

	displays := make([]string, 0, len(items))
	for _, item := range items {
		displays = append(displays, item.Display)
	}

	var selection bytes.Buffer
	command := exec.Command(finder.executable, finder.arguments...)
	command.Stdin = strings.NewReader(strings.Join(displays, "\n") + "\n")
	command.Stdout = &selection
	command.Stderr = stderr

	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			switch exitError.ExitCode() {
			case 1, 130:
				return exitError.ExitCode(), nil
			}
		}
		return 1, fmt.Errorf("run %s: %w", finder.executable, err)
	}
	value, err := selectedValue(selection.String(), items)
	if err != nil {
		return 1, err
	}
	if err := writeSelectedValue(stdout, value); err != nil {
		return 1, err
	}
	return 0, nil
}

func writeSelectedValue(output io.Writer, value string) error {
	if _, err := fmt.Fprintln(output, value); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

func selectedValue(output string, items []Item) (string, error) {
	selected := strings.Split(strings.TrimRight(output, "\r\n"), "\n")[0]
	if selected == "" {
		return "", fmt.Errorf("fuzzy finder returned no selection")
	}
	for _, item := range items {
		if selected == item.Display {
			return item.Value, nil
		}
	}
	return "", fmt.Errorf("fuzzy finder returned an unknown selection %q", selected)
}
