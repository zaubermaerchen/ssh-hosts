package sshconfig

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

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
