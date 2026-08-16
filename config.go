package main

import (
	"bufio"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const maxIncludeDepth = 16

type expansionContext struct {
	homeDir       string
	username      string
	uid           string
	localHostname string
	localShort    string
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

type hostLoader struct {
	ctx    expansionContext
	hosts  []string
	seen   map[string]struct{}
	active map[string]struct{}
}

func newHostLoader(ctx expansionContext) *hostLoader {
	return &hostLoader{
		ctx:    ctx,
		seen:   make(map[string]struct{}),
		active: make(map[string]struct{}),
	}
}

func (l *hostLoader) load(configPath string) ([]string, error) {
	if err := l.loadFile(configPath, 0); err != nil {
		return nil, err
	}
	return l.hosts, nil
}

func (l *hostLoader) loadFile(path string, depth int) error {
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
		case "include":
			if len(arguments) == 0 {
				return fmt.Errorf("%s:%d: Include requires at least one path", canonicalPath, lineNumber)
			}
			if err := l.loadIncludes(arguments, depth, canonicalPath, lineNumber); err != nil {
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
		l.hosts = append(l.hosts, host)
	}
}

func (l *hostLoader) loadIncludes(patterns []string, depth int, source string, line int) error {
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
			if err := l.loadFile(match, depth+1); err != nil {
				return fmt.Errorf("%s:%d: Include %q: %w", source, line, rawPattern, err)
			}
		}
	}
	return nil
}

func (l *hostLoader) expandIncludePath(path string) (string, error) {
	expanded, err := expandEnvironment(path)
	if err != nil {
		return "", err
	}
	expanded, err = l.expandTokens(expanded)
	if err != nil {
		return "", err
	}
	return expandTilde(expanded, l.ctx.homeDir)
}

func expandEnvironment(value string) (string, error) {
	var result strings.Builder
	for index := 0; index < len(value); {
		start := strings.Index(value[index:], "${")
		if start < 0 {
			result.WriteString(value[index:])
			break
		}
		start += index
		result.WriteString(value[index:start])
		endOffset := strings.IndexByte(value[start+2:], '}')
		if endOffset < 0 {
			return "", fmt.Errorf("unterminated environment variable")
		}
		end := start + 2 + endOffset
		name := value[start+2 : end]
		if name == "" {
			return "", fmt.Errorf("empty environment variable name")
		}
		replacement, exists := os.LookupEnv(name)
		if !exists {
			return "", fmt.Errorf("environment variable %s is not set", name)
		}
		result.WriteString(replacement)
		index = end + 1
	}
	return result.String(), nil
}

func (l *hostLoader) expandTokens(value string) (string, error) {
	replacements := map[byte]string{
		'%': "%",
		'd': l.ctx.homeDir,
		'i': l.ctx.uid,
		'L': l.ctx.localShort,
		'l': l.ctx.localHostname,
		'u': l.ctx.username,
	}
	var result strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] != '%' {
			result.WriteByte(value[index])
			continue
		}
		if index+1 == len(value) {
			return "", fmt.Errorf("incomplete %% token")
		}
		index++
		token := value[index]
		if replacement, ok := replacements[token]; ok {
			result.WriteString(replacement)
			continue
		}
		switch token {
		case 'C', 'h', 'j', 'k', 'n', 'p', 'r':
			return "", fmt.Errorf("token %%%c requires a destination host and cannot be expanded", token)
		default:
			return "", fmt.Errorf("unsupported token %%%c", token)
		}
	}
	return result.String(), nil
}

func expandTilde(path, currentHome string) (string, error) {
	if path == "~" {
		return currentHome, nil
	}
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	separator := strings.IndexAny(path, `/\\`)
	if separator < 0 {
		separator = len(path)
	}
	username := path[1:separator]
	home := currentHome
	if username != "" {
		account, err := user.Lookup(username)
		if err != nil {
			return "", fmt.Errorf("look up user %q: %w", username, err)
		}
		home = account.HomeDir
	}
	if separator == len(path) {
		return home, nil
	}
	return filepath.Join(home, path[separator+1:]), nil
}

func hasGlobMeta(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

func parseDirective(line string) (string, []string, error) {
	line = strings.TrimLeftFunc(line, unicode.IsSpace)
	if line == "" || line[0] == '#' {
		return "", nil, nil
	}

	keywordEnd := 0
	for keywordEnd < len(line) && line[keywordEnd] != '=' && !unicode.IsSpace(rune(line[keywordEnd])) {
		keywordEnd++
	}
	keyword := line[:keywordEnd]
	rest := strings.TrimLeftFunc(line[keywordEnd:], unicode.IsSpace)
	if strings.HasPrefix(rest, "=") {
		rest = strings.TrimLeftFunc(rest[1:], unicode.IsSpace)
	}
	arguments, err := splitArguments(rest)
	if err != nil {
		return "", nil, err
	}
	return keyword, arguments, nil
}

func splitArguments(value string) ([]string, error) {
	var arguments []string
	var current strings.Builder
	var quote rune
	tokenStarted := false
	escaped := false

	for _, char := range value {
		if escaped {
			current.WriteRune(char)
			tokenStarted = true
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			tokenStarted = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				current.WriteRune(char)
			}
			continue
		}
		if char == '"' {
			quote = char
			tokenStarted = true
			continue
		}
		if char == '#' {
			break
		}
		if unicode.IsSpace(char) {
			if tokenStarted {
				arguments = append(arguments, current.String())
				current.Reset()
				tokenStarted = false
			}
			continue
		}
		current.WriteRune(char)
		tokenStarted = true
	}
	if escaped {
		return nil, fmt.Errorf("trailing escape")
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %s quote", strconv.QuoteRune(quote))
	}
	if tokenStarted {
		arguments = append(arguments, current.String())
	}
	return arguments, nil
}
