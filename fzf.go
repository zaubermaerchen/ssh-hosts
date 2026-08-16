package main

import (
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

var supportedFinders = []finderSpec{
	{name: "fzf", executable: "fzf", arguments: []string{"--prompt=SSH host> "}},
	{name: "sk", executable: "sk", arguments: []string{"--prompt=SSH host> "}},
	{
		name:       "peco",
		executable: "peco",
		arguments:  []string{"--prompt=SSH host> ", "--initial-filter=Fuzzy", "--on-cancel=error"},
	},
}

func normalizeFinder(name string) (string, error) {
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

func selectWithFZF(hosts []string, finderName string, stdout, stderr io.Writer) (int, error) {
	if len(hosts) == 0 {
		return 1, nil
	}
	finder, err := resolveFinder(finderName)
	if err != nil {
		return 1, err
	}

	command := exec.Command(finder.executable, finder.arguments...)
	command.Stdin = strings.NewReader(strings.Join(hosts, "\n") + "\n")
	command.Stdout = stdout
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
	return 0, nil
}
