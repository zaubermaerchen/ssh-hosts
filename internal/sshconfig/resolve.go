package sshconfig

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

type hostCondition struct {
	clauses [][]string
	none    bool
}

type optionDirective struct {
	condition hostCondition
	keyword   string
	value     string
	source    string
	line      int
}

func (l *hostLoader) resolveHosts() ([]Host, error) {
	hosts := make([]Host, 0, len(l.aliases))
	for _, alias := range l.aliases {
		host := Host{Alias: alias}
		var hostNameSource, userSource, portSource *optionDirective
		for index := range l.options {
			option := &l.options[index]
			matches, err := option.condition.matches(alias)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid Host pattern: %w", option.source, option.line, err)
			}
			if !matches {
				continue
			}
			switch option.keyword {
			case "hostname":
				if host.HostName == "" {
					host.HostName = option.value
					hostNameSource = option
				}
			case "user":
				if host.User == "" {
					host.User = option.value
					userSource = option
				}
			case "port":
				if host.Port == "" {
					host.Port = option.value
					portSource = option
				}
			}
		}
		if host.User == "" {
			host.User = l.ctx.username
		}
		if host.HostName == "" {
			host.HostName = alias
		} else {
			expanded, err := expandHostName(host.HostName, alias)
			if err != nil {
				return nil, optionError(hostNameSource, "resolve HostName for %q: %v", alias, err)
			}
			host.HostName = expanded
		}
		if host.Port == "" {
			host.Port = "22"
		}
		port, err := strconv.Atoi(host.Port)
		if err != nil || port < 1 || port > 65535 {
			return nil, optionError(portSource, "resolve Port for %q: invalid port %q", alias, host.Port)
		}
		expandedUser, err := l.expandUser(host.User, host)
		if err != nil {
			return nil, optionError(userSource, "resolve User for %q: %v", alias, err)
		}
		host.User = expandedUser
		hosts = append(hosts, host)
	}
	return hosts, nil
}

func optionError(source *optionDirective, format string, arguments ...any) error {
	message := fmt.Sprintf(format, arguments...)
	if source == nil {
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("%s:%d: %s", source.source, source.line, message)
}

func (c hostCondition) matches(alias string) (bool, error) {
	if c.none {
		return false, nil
	}
	for _, clause := range c.clauses {
		matchedPositive := false
		for _, argument := range clause {
			for _, rawPattern := range strings.Split(argument, ",") {
				negated := strings.HasPrefix(rawPattern, "!")
				pattern := strings.TrimPrefix(rawPattern, "!")
				matched, err := filepath.Match(strings.ToLower(pattern), strings.ToLower(alias))
				if err != nil {
					return false, err
				}
				if matched && negated {
					return false, nil
				}
				if matched {
					matchedPositive = true
				}
			}
		}
		if !matchedPositive {
			return false, nil
		}
	}
	return true, nil
}

func combineConditions(left, right hostCondition) hostCondition {
	if left.none || right.none {
		return hostCondition{none: true}
	}
	clauses := make([][]string, 0, len(left.clauses)+len(right.clauses))
	clauses = append(clauses, left.clauses...)
	clauses = append(clauses, right.clauses...)
	return hostCondition{clauses: clauses}
}
