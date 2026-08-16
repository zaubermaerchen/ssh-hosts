package main

import (
	"bytes"
	"path/filepath"
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
		})
	}
}
