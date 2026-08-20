package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zaubermaerchen/ssh-hosts/internal/finder"
	"github.com/zaubermaerchen/ssh-hosts/internal/sshconfig"
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
	if stdout.String() != "default-host\n" {
		t.Fatalf("stdout = %q, want alias-only output", stdout.String())
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
	if code != 0 || stdout.String() != "explicit-host\n" {
		t.Fatalf("run() code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestRunWithDetails(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom-config")
	writeTestFile(t, configPath, `
Host production
    User deploy
    HostName prod.example.com
    Port 2222
Host staging
    User ubuntu
    HostName 10.0.0.12
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--details", configPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	if want := "production\tdeploy@prod.example.com:2222\nstaging\tubuntu@10.0.0.12:22\n"; stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestRunWithJSON(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom-config")
	writeTestFile(t, configPath, `
Host production
    User deploy
    HostName prod.example.com
    Port 2222
Host ipv6
    User root
    HostName 2001:db8::10
`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"--json", configPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() code = %d, stderr = %q", code, stderr.String())
	}
	var got []jsonHost
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("JSON output = %q: %v", stdout.String(), err)
	}
	want := []jsonHost{
		{Alias: "production", User: "deploy", Hostname: "prod.example.com", Port: 2222, Destination: "deploy@prod.example.com:2222"},
		{Alias: "ipv6", User: "root", Hostname: "2001:db8::10", Port: 22, Destination: "root@[2001:db8::10]:22"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON hosts = %#v, want %#v", got, want)
	}
	if !strings.Contains(stdout.String(), "\n  {") {
		t.Fatalf("JSON output is not indented: %q", stdout.String())
	}
}

func TestWriteJSONHostsWithNoHosts(t *testing.T) {
	var output bytes.Buffer
	if err := writeJSONHosts(&output, nil); err != nil {
		t.Fatalf("writeJSONHosts() error = %v", err)
	}
	if output.String() != "[]\n" {
		t.Fatalf("output = %q, want []", output.String())
	}
}

func TestWriteJSONHostsRejectsInvalidPort(t *testing.T) {
	var output bytes.Buffer
	err := writeJSONHosts(&output, []sshconfig.Host{{Alias: "broken", Port: "invalid"}})
	if err == nil || !strings.Contains(err.Error(), "invalid port") {
		t.Fatalf("writeJSONHosts() error = %v, want invalid port error", err)
	}
}

func TestRunWithSelect(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom-config")
	writeTestFile(t, configPath, "Host alpha wildcard-* beta\nUser tester\nHostName %h.example\nPort 2200\n")

	var receivedItems []finder.Item
	selector := func(items []finder.Item, stdout, stderr io.Writer) (int, error) {
		receivedItems = append([]finder.Item(nil), items...)
		fmt.Fprintln(stdout, "beta")
		return 0, nil
	}
	var stdout, stderr bytes.Buffer
	code := runWithSelector([]string{"--select", configPath}, &stdout, &stderr, selector)
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
	if stdout.String() != "beta\n" {
		t.Fatalf("stdout = %q, want %q", stdout.String(), "beta\n")
	}
}

func TestRunWithSelectAndDetails(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom-config")
	writeTestFile(t, configPath, "Host alpha beta\nUser tester\nHostName %h.example\nPort 2200\n")

	var receivedItems []finder.Item
	selector := func(items []finder.Item, stdout, stderr io.Writer) (int, error) {
		receivedItems = append([]finder.Item(nil), items...)
		fmt.Fprintln(stdout, items[1].Value)
		return 0, nil
	}
	var stdout, stderr bytes.Buffer
	code := runWithSelector([]string{"--details", "--select", configPath}, &stdout, &stderr, selector)
	if code != 0 {
		t.Fatalf("runWithSelector() code = %d, stderr = %q", code, stderr.String())
	}
	wantItems := []finder.Item{
		{Value: "alpha\ttester@alpha.example:2200", Display: "alpha\ttester@alpha.example:2200"},
		{Value: "beta\ttester@beta.example:2200", Display: "beta\ttester@beta.example:2200"},
	}
	if !reflect.DeepEqual(receivedItems, wantItems) {
		t.Fatalf("selector items = %#v, want %#v", receivedItems, wantItems)
	}
	if stdout.String() != "beta\ttester@beta.example:2200\n" {
		t.Fatalf("stdout = %q, want detailed selection", stdout.String())
	}
}

func TestRunWithSelectError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom-config")
	writeTestFile(t, configPath, "Host alpha\n")
	wantError := errors.New("terminal unavailable")
	selector := func([]finder.Item, io.Writer, io.Writer) (int, error) {
		return 1, wantError
	}

	var stdout, stderr bytes.Buffer
	code := runWithSelector([]string{"--select", configPath}, &stdout, &stderr, selector)
	if code != 1 || !strings.Contains(stderr.String(), wantError.Error()) {
		t.Fatalf("runWithSelector() code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunWithSelectCancellationHasNoError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom-config")
	writeTestFile(t, configPath, "Host alpha\n")
	selector := func([]finder.Item, io.Writer, io.Writer) (int, error) {
		return 1, nil
	}

	var stdout, stderr bytes.Buffer
	code := runWithSelector([]string{"--select", configPath}, &stdout, &stderr, selector)
	if code == 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("runWithSelector() code = %d, stdout = %q, stderr = %q", code, stdout.String(), stderr.String())
	}
}

func TestRemovedFinderOptionsAreRejected(t *testing.T) {
	for _, args := range [][]string{{"--fzf"}, {"--finder=fzf"}} {
		selectorCalled := false
		selector := func([]finder.Item, io.Writer, io.Writer) (int, error) {
			selectorCalled = true
			return 0, nil
		}

		var stdout, stderr bytes.Buffer
		code := runWithSelector(args, &stdout, &stderr, selector)
		if code != 2 || selectorCalled || stdout.Len() != 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Fatalf("args = %#v, code = %d, selectorCalled = %v, stdout = %q, stderr = %q", args, code, selectorCalled, stdout.String(), stderr.String())
		}
	}
}

func TestJSONCannotBeCombinedWithSelect(t *testing.T) {
	for _, args := range [][]string{{"--json", "--select"}, {"--select", "--json"}} {
		selectorCalled := false
		selector := func([]finder.Item, io.Writer, io.Writer) (int, error) {
			selectorCalled = true
			return 0, nil
		}
		var stdout, stderr bytes.Buffer
		code := runWithSelector(args, &stdout, &stderr, selector)
		if code != 2 || selectorCalled || !strings.Contains(stderr.String(), "--json cannot be combined with --select") {
			t.Fatalf("args = %#v, code = %d, selectorCalled = %v, stderr = %q", args, code, selectorCalled, stderr.String())
		}
	}
}

func TestDetailsCannotBeCombinedWithJSON(t *testing.T) {
	for _, args := range [][]string{
		{"--details", "--json"},
		{"--json", "--details"},
	} {
		selectorCalled := false
		selector := func([]finder.Item, io.Writer, io.Writer) (int, error) {
			selectorCalled = true
			return 0, nil
		}
		var stdout, stderr bytes.Buffer
		code := runWithSelector(args, &stdout, &stderr, selector)
		if code != 2 || selectorCalled || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--details cannot be combined with --json") {
			t.Fatalf("args = %#v, code = %d, selectorCalled = %v, stderr = %q", args, code, selectorCalled, stderr.String())
		}
	}
}

func TestRunReportsOutputError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "custom-config")
	writeTestFile(t, configPath, "Host alpha\n")
	wantError := errors.New("output unavailable")

	for _, args := range [][]string{{configPath}, {"--details", configPath}} {
		var stderr bytes.Buffer
		code := run(args, errorWriter{err: wantError}, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "write output") || !strings.Contains(stderr.String(), wantError.Error()) {
			t.Fatalf("args = %#v, code = %d, stderr = %q", args, code, stderr.String())
		}
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
			if !strings.Contains(stderr.String(), "-select") {
				t.Fatalf("stderr = %q, want select option", stderr.String())
			}
			if !strings.Contains(stderr.String(), "-json") {
				t.Fatalf("stderr = %q, want json option", stderr.String())
			}
			if !strings.Contains(stderr.String(), "-details") {
				t.Fatalf("stderr = %q, want details option", stderr.String())
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

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}
