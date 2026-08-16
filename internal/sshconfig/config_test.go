package sshconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadHostsWithIncludes(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	sshDir := filepath.Join(homeDir, ".ssh")
	extraConfig := filepath.Join(tempDir, "extra.conf")
	t.Setenv("EXTRA_CONFIG", extraConfig)

	writeTestFile(t, filepath.Join(sshDir, "config"), `
# Keywords are case-insensitive and may use an equals sign.
HoSt = first wildcard-* exact-two !negated host? "quoted host" exact-two
Include conf.d/*.conf "${EXTRA_CONFIG}" ~/.ssh/tilde.conf %d/.ssh/token.conf missing/*.conf
Host last
`)
	writeTestFile(t, filepath.Join(sshDir, "conf.d", "10-first.conf"), `
Host included-10
Include nested.conf
`)
	writeTestFile(t, filepath.Join(sshDir, "nested.conf"), "Host nested\n")
	writeTestFile(t, filepath.Join(sshDir, "conf.d", "20-second.conf"), `
Host included-20
Match all
    Include conditional.conf
`)
	writeTestFile(t, filepath.Join(sshDir, "conditional.conf"), "Host conditional\n")
	writeTestFile(t, extraConfig, "Host extra\n")
	writeTestFile(t, filepath.Join(sshDir, "tilde.conf"), "Host tilde\n")
	writeTestFile(t, filepath.Join(sshDir, "token.conf"), "Host token\n")

	ctx := expansionContext{
		homeDir:       homeDir,
		username:      "alice",
		uid:           "501",
		localHostname: "workstation.example.com",
		localShort:    "workstation",
	}
	hosts, err := newHostLoader(ctx).load(filepath.Join(sshDir, "config"))
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	wantAliases := []string{
		"first", "exact-two", "quoted host", "included-10", "nested",
		"included-20", "conditional", "extra", "tilde", "token", "last",
	}
	if got := hostAliases(hosts); !reflect.DeepEqual(got, wantAliases) {
		t.Fatalf("aliases = %#v, want %#v", got, wantAliases)
	}
	for _, host := range hosts {
		if host.User != "alice" || host.HostName != host.Alias || host.Port != "22" {
			t.Errorf("host = %#v, want default destination values", host)
		}
	}
}

func TestResolveConnectionOptions(t *testing.T) {
	tempDir := t.TempDir()
	sshDir := filepath.Join(tempDir, ".ssh")
	configPath := filepath.Join(sshDir, "config")
	writeTestFile(t, configPath, `
Host production
    User deploy
    HostName prod.example.com
    Port 2222
Host production
    HostName ignored.example.com
Host staging
    HostName 10.0.0.12
Host ipv6
    User root
    HostName 2001:db8::10
Host templated
    HostName %h.internal
Host included
    Include destination.conf
Host dynamic
Match exec "false"
    User ignored
Match all
    User fallback
    Port 22
`)
	writeTestFile(t, filepath.Join(sshDir, "destination.conf"), "HostName included.example.com\nPort 2200\n")

	ctx := expansionContext{homeDir: tempDir, username: "local-user"}
	hosts, err := newHostLoader(ctx).load(configPath)
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	want := []Host{
		{Alias: "production", User: "deploy", HostName: "prod.example.com", Port: "2222"},
		{Alias: "staging", User: "fallback", HostName: "10.0.0.12", Port: "22"},
		{Alias: "ipv6", User: "root", HostName: "2001:db8::10", Port: "22"},
		{Alias: "templated", User: "fallback", HostName: "templated.internal", Port: "22"},
		{Alias: "included", User: "fallback", HostName: "included.example.com", Port: "2200"},
		{Alias: "dynamic", User: "fallback", HostName: "dynamic", Port: "22"},
	}
	if !reflect.DeepEqual(hosts, want) {
		t.Fatalf("hosts = %#v, want %#v", hosts, want)
	}
	if got := hosts[2].Destination(); got != "root@[2001:db8::10]:22" {
		t.Fatalf("IPv6 destination = %q, want %q", got, "root@[2001:db8::10]:22")
	}
}

func TestResolveWildcardOptionsAndUserExpansion(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("SSH_HOSTS_TEST_USER", "env-user")
	configPath := filepath.Join(tempDir, ".ssh", "config")
	writeTestFile(t, configPath, `
Host app-one excluded token-user
Host app-*
    HostName %h.example.com
Host * !excluded
    Port 2200
Host token-user
    User ${SSH_HOSTS_TEST_USER}-%u-%p
Host *
    User local-default
    Port 22
`)

	ctx := expansionContext{
		homeDir:       tempDir,
		username:      "local",
		uid:           "501",
		localHostname: "workstation.example.com",
		localShort:    "workstation",
	}
	hosts, err := newHostLoader(ctx).load(configPath)
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	want := []Host{
		{Alias: "app-one", User: "local-default", HostName: "app-one.example.com", Port: "2200"},
		{Alias: "excluded", User: "local-default", HostName: "excluded", Port: "22"},
		{Alias: "token-user", User: "env-user-local-2200", HostName: "token-user", Port: "2200"},
	}
	if !reflect.DeepEqual(hosts, want) {
		t.Fatalf("hosts = %#v, want %#v", hosts, want)
	}
}

func TestConditionalIncludePreservesParentHostState(t *testing.T) {
	tempDir := t.TempDir()
	sshDir := filepath.Join(tempDir, ".ssh")
	configPath := filepath.Join(sshDir, "config")
	writeTestFile(t, configPath, `
Host foo
    Include child.conf
    Port 2222
`)
	writeTestFile(t, filepath.Join(sshDir, "child.conf"), `
Host bar
    HostName must-not-apply.example
`)

	hosts, err := newHostLoader(expansionContext{homeDir: tempDir, username: "alice"}).load(configPath)
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	want := []Host{
		{Alias: "foo", User: "alice", HostName: "foo", Port: "2222"},
		{Alias: "bar", User: "alice", HostName: "bar", Port: "22"},
	}
	if !reflect.DeepEqual(hosts, want) {
		t.Fatalf("hosts = %#v, want %#v", hosts, want)
	}
}

func TestExpandTokens(t *testing.T) {
	loader := newHostLoader(expansionContext{
		homeDir:       "/home/alice",
		username:      "alice",
		uid:           "1001",
		localHostname: "host.example.com",
		localShort:    "host",
	})
	got, err := loader.expandTokens("%%-%d-%u-%i-%l-%L")
	if err != nil {
		t.Fatalf("expandTokens() error = %v", err)
	}
	want := "%-/home/alice-alice-1001-host.example.com-host"
	if got != want {
		t.Fatalf("expandTokens() = %q, want %q", got, want)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name        string
		config      string
		prepare     func(t *testing.T, sshDir string)
		wantMessage string
	}{
		{
			name:        "undefined environment variable",
			config:      "Include ${SSH_HOSTS_UNDEFINED}/config\n",
			wantMessage: "environment variable SSH_HOSTS_UNDEFINED is not set",
		},
		{
			name:        "destination token",
			config:      "Include %h.conf\n",
			wantMessage: "token %h requires a destination host",
		},
		{
			name:        "missing explicit file",
			config:      "Include missing.conf\n",
			wantMessage: "missing.conf",
		},
		{
			name:        "invalid glob",
			config:      "Include [invalid\n",
			wantMessage: "syntax error in pattern",
		},
		{
			name:   "include cycle",
			config: "Include cycle.conf\n",
			prepare: func(t *testing.T, sshDir string) {
				writeTestFile(t, filepath.Join(sshDir, "cycle.conf"), "Include config\n")
			},
			wantMessage: "include cycle detected",
		},
		{
			name:        "unterminated quote",
			config:      "Host \"broken\n",
			wantMessage: "unterminated",
		},
		{
			name:        "invalid port",
			config:      "Host broken\nPort 70000\n",
			wantMessage: "invalid port",
		},
		{
			name:        "unsupported user token",
			config:      "Host broken\nUser %r\n",
			wantMessage: "unsupported token %r",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			sshDir := filepath.Join(tempDir, ".ssh")
			configPath := filepath.Join(sshDir, "config")
			writeTestFile(t, configPath, test.config)
			if test.prepare != nil {
				test.prepare(t, sshDir)
			}
			ctx := expansionContext{homeDir: tempDir}
			_, err := newHostLoader(ctx).load(configPath)
			if err == nil || !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("load() error = %v, want message containing %q", err, test.wantMessage)
			}
			if !strings.Contains(err.Error(), configPath) {
				t.Fatalf("load() error = %v, want source path", err)
			}
		})
	}
}

func TestUnmatchedIncludeGlobIsIgnored(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, ".ssh", "config")
	writeTestFile(t, configPath, "Include absent/*.conf\nHost available\n")

	hosts, err := newHostLoader(expansionContext{homeDir: tempDir, username: "alice"}).load(configPath)
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if want := []Host{{Alias: "available", User: "alice", HostName: "available", Port: "22"}}; !reflect.DeepEqual(hosts, want) {
		t.Fatalf("hosts = %#v, want %#v", hosts, want)
	}
}

func hostAliases(hosts []Host) []string {
	aliases := make([]string, 0, len(hosts))
	for _, host := range hosts {
		aliases = append(aliases, host.Alias)
	}
	return aliases
}

func TestIncludeDepthLimit(t *testing.T) {
	tempDir := t.TempDir()
	sshDir := filepath.Join(tempDir, ".ssh")
	configPath := filepath.Join(sshDir, "config")
	writeTestFile(t, configPath, "Include depth-00.conf\n")
	for index := 0; index <= maxIncludeDepth; index++ {
		current := filepath.Join(sshDir, fmt.Sprintf("depth-%02d.conf", index))
		next := fmt.Sprintf("depth-%02d.conf", index+1)
		writeTestFile(t, current, "Include "+next+"\n")
	}

	_, err := newHostLoader(expansionContext{homeDir: tempDir}).load(configPath)
	if err == nil || !strings.Contains(err.Error(), "include depth exceeds") {
		t.Fatalf("load() error = %v, want include depth error", err)
	}
}

func TestParseDirective(t *testing.T) {
	keyword, arguments, err := parseDirective(` Host = one "two three" four\ five # comment`)
	if err != nil {
		t.Fatalf("parseDirective() error = %v", err)
	}
	if keyword != "Host" {
		t.Fatalf("keyword = %q, want Host", keyword)
	}
	want := []string{"one", "two three", "four five"}
	if !reflect.DeepEqual(arguments, want) {
		t.Fatalf("arguments = %#v, want %#v", arguments, want)
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
