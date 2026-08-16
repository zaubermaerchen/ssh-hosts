package sshconfig

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

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
