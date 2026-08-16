package sshconfig

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

type expansionContext struct {
	homeDir       string
	username      string
	uid           string
	localHostname string
	localShort    string
}

// Loader reads OpenSSH client configuration using the current user's
// environment for Include path and token expansion.
type Loader struct {
	ctx expansionContext
}

// Host is a concrete SSH alias and its resolved connection destination.
type Host struct {
	Alias    string
	User     string
	HostName string
	Port     string
}

// Destination formats the resolved destination for display.
func (h Host) Destination() string {
	hostname := h.HostName
	if strings.HasPrefix(hostname, "[") && strings.HasSuffix(hostname, "]") {
		hostname = strings.TrimSuffix(strings.TrimPrefix(hostname, "["), "]")
	}
	return h.User + "@" + net.JoinHostPort(hostname, h.Port)
}

// NewLoader creates a Loader configured for the current user and machine.
func NewLoader() (*Loader, error) {
	ctx, err := systemExpansionContext()
	if err != nil {
		return nil, err
	}
	return &Loader{ctx: ctx}, nil
}

// DefaultConfigPath returns the current user's default SSH config path.
func (l *Loader) DefaultConfigPath() string {
	return filepath.Join(l.ctx.homeDir, ".ssh", "config")
}

// ListHosts returns concrete Host entries and their resolved destinations in
// first-seen order.
func (l *Loader) ListHosts(configPath string) ([]Host, error) {
	return newHostLoader(l.ctx).load(configPath)
}

func systemExpansionContext() (expansionContext, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return expansionContext{}, fmt.Errorf("determine home directory: %w", err)
	}
	currentUser, err := user.Current()
	if err != nil {
		return expansionContext{}, fmt.Errorf("determine current user: %w", err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		return expansionContext{}, fmt.Errorf("determine local hostname: %w", err)
	}
	short := hostname
	if before, _, found := strings.Cut(hostname, "."); found {
		short = before
	}
	return expansionContext{
		homeDir:       homeDir,
		username:      currentUser.Username,
		uid:           currentUser.Uid,
		localHostname: hostname,
		localShort:    short,
	}, nil
}
