package sshconfig

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxIncludeDepth = 16

type hostLoader struct {
	ctx     expansionContext
	aliases []string
	options []optionDirective
	seen    map[string]struct{}
	active  map[string]struct{}
}

type parseState struct {
	guard     hostCondition
	condition hostCondition
}

func newHostLoader(ctx expansionContext) *hostLoader {
	return &hostLoader{
		ctx:    ctx,
		seen:   make(map[string]struct{}),
		active: make(map[string]struct{}),
	}
}

func (l *hostLoader) load(configPath string) ([]Host, error) {
	state := &parseState{}
	if err := l.loadFile(configPath, 0, state); err != nil {
		return nil, err
	}
	return l.resolveHosts()
}

func (l *hostLoader) loadFile(path string, depth int, state *parseState) error {
	if depth > maxIncludeDepth {
		return fmt.Errorf("include depth exceeds %d while reading %q", maxIncludeDepth, path)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", path, err)
	}
	canonicalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return fmt.Errorf("read %q: %w", path, err)
	}
	if _, exists := l.active[canonicalPath]; exists {
		return fmt.Errorf("include cycle detected at %q", canonicalPath)
	}

	file, err := os.Open(canonicalPath)
	if err != nil {
		return fmt.Errorf("read %q: %w", path, err)
	}
	defer file.Close()

	l.active[canonicalPath] = struct{}{}
	defer delete(l.active, canonicalPath)

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		keyword, arguments, err := parseDirective(scanner.Text())
		if err != nil {
			return fmt.Errorf("%s:%d: %w", canonicalPath, lineNumber, err)
		}
		switch strings.ToLower(keyword) {
		case "host":
			if len(arguments) == 0 {
				return fmt.Errorf("%s:%d: Host requires at least one argument", canonicalPath, lineNumber)
			}
			l.addHosts(arguments)
			state.condition = combineConditions(state.guard, hostCondition{clauses: [][]string{arguments}})
		case "match":
			if len(arguments) == 0 {
				return fmt.Errorf("%s:%d: Match requires at least one argument", canonicalPath, lineNumber)
			}
			if len(arguments) == 1 && strings.EqualFold(arguments[0], "all") {
				state.condition = state.guard
			} else {
				// Match may depend on runtime state or execute commands. Do not
				// apply its options while producing a static host listing.
				state.condition = hostCondition{none: true}
			}
		case "hostname", "user", "port":
			if len(arguments) != 1 {
				return fmt.Errorf("%s:%d: %s requires exactly one argument", canonicalPath, lineNumber, keyword)
			}
			l.options = append(l.options, optionDirective{
				condition: state.condition,
				keyword:   strings.ToLower(keyword),
				value:     arguments[0],
				source:    canonicalPath,
				line:      lineNumber,
			})
		case "include":
			if len(arguments) == 0 {
				return fmt.Errorf("%s:%d: Include requires at least one path", canonicalPath, lineNumber)
			}
			if err := l.loadIncludes(arguments, depth, canonicalPath, lineNumber, state); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %q: %w", canonicalPath, err)
	}
	return nil
}

func (l *hostLoader) addHosts(arguments []string) {
	for _, host := range arguments {
		if host == "" || strings.HasPrefix(host, "!") || strings.ContainsAny(host, "*?[") {
			continue
		}
		if _, exists := l.seen[host]; exists {
			continue
		}
		l.seen[host] = struct{}{}
		l.aliases = append(l.aliases, host)
	}
}

func (l *hostLoader) loadIncludes(patterns []string, depth int, source string, line int, state *parseState) error {
	for _, rawPattern := range patterns {
		pattern, err := l.expandIncludePath(rawPattern)
		if err != nil {
			return fmt.Errorf("%s:%d: Include %q: %w", source, line, rawPattern, err)
		}
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(l.ctx.homeDir, ".ssh", pattern)
		}

		matches, err := filepath.Glob(pattern)
		if err != nil {
			return fmt.Errorf("%s:%d: Include %q: %w", source, line, rawPattern, err)
		}
		// filepath.Glob already sorts, but keep the ordering guarantee explicit.
		sort.Strings(matches)
		if len(matches) == 0 {
			if hasGlobMeta(pattern) {
				continue
			}
			matches = []string{pattern}
		}
		for _, match := range matches {
			includedState := &parseState{guard: state.condition, condition: state.condition}
			if err := l.loadFile(match, depth+1, includedState); err != nil {
				return fmt.Errorf("%s:%d: Include %q: %w", source, line, rawPattern, err)
			}
		}
	}
	return nil
}
