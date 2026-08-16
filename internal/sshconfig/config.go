package sshconfig

import (
	"bufio"
	"fmt"
	"net"
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

type hostLoader struct {
	ctx     expansionContext
	aliases []string
	options []optionDirective
	seen    map[string]struct{}
	active  map[string]struct{}
}

type hostCondition struct {
	clauses [][]string
	none    bool
}

type parseState struct {
	guard     hostCondition
	condition hostCondition
}

type optionDirective struct {
	condition hostCondition
	keyword   string
	value     string
	source    string
	line      int
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

func (l *hostLoader) expandUser(value string, host Host) (string, error) {
	expanded, err := expandEnvironment(value)
	if err != nil {
		return "", err
	}
	replacements := map[byte]string{
		'%': "%",
		'd': l.ctx.homeDir,
		'h': host.HostName,
		'i': l.ctx.uid,
		'k': host.Alias,
		'L': l.ctx.localShort,
		'l': l.ctx.localHostname,
		'n': host.Alias,
		'p': host.Port,
		'u': l.ctx.username,
	}
	var result strings.Builder
	for index := 0; index < len(expanded); index++ {
		if expanded[index] != '%' {
			result.WriteByte(expanded[index])
			continue
		}
		if index+1 == len(expanded) {
			return "", fmt.Errorf("incomplete %% token")
		}
		index++
		if replacement, ok := replacements[expanded[index]]; ok {
			result.WriteString(replacement)
			continue
		}
		return "", fmt.Errorf("unsupported token %%%c", expanded[index])
	}
	if result.Len() == 0 {
		return "", fmt.Errorf("User expands to an empty value")
	}
	return result.String(), nil
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

func expandHostName(value, alias string) (string, error) {
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
		switch value[index] {
		case '%':
			result.WriteByte('%')
		case 'h':
			result.WriteString(alias)
		default:
			return "", fmt.Errorf("unsupported token %%%c", value[index])
		}
	}
	return result.String(), nil
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
