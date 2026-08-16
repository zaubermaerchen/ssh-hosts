package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunUsesDefaultConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeTestFile(t, filepath.Join(homeDir, ".ssh", "config"), "Host default-host\n")

	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "default-host\n" {
		t.Fatalf("stdout = %q, want %q", stdout.String(), "default-host\n")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunWithExplicitConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom-config")
	writeTestFile(t, configPath, "Host explicit-host\n")

	var stdout, stderr bytes.Buffer
	code := run([]string{configPath}, &stdout, &stderr)
	if code != 0 || stdout.String() != "explicit-host\n" {
		t.Fatalf("run() code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestRunWithFZF(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom-config")
	writeTestFile(t, configPath, "Host alpha wildcard-* beta\n")

	var receivedHosts []string
	var receivedFinder string
	selector := func(hosts []string, finder string, stdout, stderr io.Writer) (int, error) {
		receivedHosts = append([]string(nil), hosts...)
		receivedFinder = finder
		fmt.Fprintln(stdout, "beta")
		return 0, nil
	}
	var stdout, stderr bytes.Buffer
	code := runWithSelector([]string{"--fzf", configPath}, &stdout, &stderr, selector)
	if code != 0 {
		t.Fatalf("runWithSelector() code = %d, stderr = %q", code, stderr.String())
	}
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(receivedHosts, want) {
		t.Fatalf("selector hosts = %#v, want %#v", receivedHosts, want)
	}
	if receivedFinder != "auto" {
		t.Fatalf("selector finder = %q, want auto", receivedFinder)
	}
	if stdout.String() != "beta\n" {
		t.Fatalf("stdout = %q, want %q", stdout.String(), "beta\n")
	}
}

func TestRunWithFZFError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom-config")
	writeTestFile(t, configPath, "Host alpha\n")
	wantError := errors.New("fzf is not installed")
	selector := func([]string, string, io.Writer, io.Writer) (int, error) {
		return 1, wantError
	}

	var stdout, stderr bytes.Buffer
	code := runWithSelector([]string{"--fzf", configPath}, &stdout, &stderr, selector)
	if code != 1 || !strings.Contains(stderr.String(), wantError.Error()) {
		t.Fatalf("runWithSelector() code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunPreservesFZFCancelCode(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom-config")
	writeTestFile(t, configPath, "Host alpha\n")
	selector := func([]string, string, io.Writer, io.Writer) (int, error) {
		return 130, nil
	}

	var stdout, stderr bytes.Buffer
	code := runWithSelector([]string{"--fzf", configPath}, &stdout, &stderr, selector)
	if code != 130 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("runWithSelector() code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestFinderOptionImpliesFZF(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom-config")
	writeTestFile(t, configPath, "Host alpha\n")
	var receivedFinder string
	selector := func(_ []string, finder string, stdout, stderr io.Writer) (int, error) {
		receivedFinder = finder
		fmt.Fprintln(stdout, "alpha")
		return 0, nil
	}

	var stdout, stderr bytes.Buffer
	code := runWithSelector([]string{"--finder=skim", configPath}, &stdout, &stderr, selector)
	if code != 0 || receivedFinder != "sk" || stdout.String() != "alpha\n" {
		t.Fatalf("code = %d, finder = %q, stdout = %q, stderr = %q", code, receivedFinder, stdout.String(), stderr.String())
	}
}

func TestInvalidFinder(t *testing.T) {
	selectorCalled := false
	selector := func([]string, string, io.Writer, io.Writer) (int, error) {
		selectorCalled = true
		return 0, nil
	}
	var stdout, stderr bytes.Buffer
	code := runWithSelector([]string{"--finder=unknown"}, &stdout, &stderr, selector)
	if code != 2 || selectorCalled || !strings.Contains(stderr.String(), "unsupported fuzzy finder") {
		t.Fatalf("code = %d, selectorCalled = %v, stderr = %q", code, selectorCalled, stderr.String())
	}
}

func TestRunUsageErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		code int
	}{
		{name: "help", args: []string{"-h"}, code: 0},
		{name: "too many arguments", args: []string{"one", "two"}, code: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(test.args, &stdout, &stderr)
			if code != test.code {
				t.Fatalf("run() code = %d, want %d", code, test.code)
			}
			if !strings.Contains(stderr.String(), "Usage: ssh-hosts") {
				t.Fatalf("stderr = %q, want usage", stderr.String())
			}
			if !strings.Contains(stderr.String(), "-fzf") {
				t.Fatalf("stderr = %q, want fzf option", stderr.String())
			}
			if !strings.Contains(stderr.String(), "-finder") {
				t.Fatalf("stderr = %q, want finder option", stderr.String())
			}
		})
	}
}
