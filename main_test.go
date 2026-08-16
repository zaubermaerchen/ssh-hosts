package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zaubermaerchen/ssh-hosts/internal/finder"
)

func TestRunUsesDefaultConfig(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	writeTestFile(t, filepath.Join(homeDir, ".ssh", "config"), "Host default-host\nUser alice\nHostName destination.example\nPort 2222\n")

	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "default-host\talice@destination.example:2222\n" {
		t.Fatalf("stdout = %q, want resolved destination", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunWithExplicitConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom-config")
	writeTestFile(t, configPath, "Host explicit-host\nUser bob\nHostName 10.0.0.1\nPort 2200\n")

	var stdout, stderr bytes.Buffer
	code := run([]string{configPath}, &stdout, &stderr)
	if code != 0 || stdout.String() != "explicit-host\tbob@10.0.0.1:2200\n" {
		t.Fatalf("run() code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestRunWithFZF(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom-config")
	writeTestFile(t, configPath, "Host alpha wildcard-* beta\nUser tester\nHostName %h.example\nPort 2200\n")

	var receivedItems []finder.Item
	var receivedFinder string
	selector := func(items []finder.Item, finderName string, stdout, stderr io.Writer) (int, error) {
		receivedItems = append([]finder.Item(nil), items...)
		receivedFinder = finderName
		fmt.Fprintln(stdout, "beta")
		return 0, nil
	}
	var stdout, stderr bytes.Buffer
	code := runWithSelector([]string{"--fzf", configPath}, &stdout, &stderr, selector)
	if code != 0 {
		t.Fatalf("runWithSelector() code = %d, stderr = %q", code, stderr.String())
	}
	wantItems := []finder.Item{
		{Value: "alpha", Display: "alpha\ttester@alpha.example:2200"},
		{Value: "beta", Display: "beta\ttester@beta.example:2200"},
	}
	if !reflect.DeepEqual(receivedItems, wantItems) {
		t.Fatalf("selector items = %#v, want %#v", receivedItems, wantItems)
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
	selector := func([]finder.Item, string, io.Writer, io.Writer) (int, error) {
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
	selector := func([]finder.Item, string, io.Writer, io.Writer) (int, error) {
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
	selector := func(_ []finder.Item, finderName string, stdout, stderr io.Writer) (int, error) {
		receivedFinder = finderName
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
	selector := func([]finder.Item, string, io.Writer, io.Writer) (int, error) {
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

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
