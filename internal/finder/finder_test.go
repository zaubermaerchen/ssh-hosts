package finder

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSelectWithFZFWithoutHosts(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code, err := Select(nil, "auto", &stdout, &stderr)
	if err != nil || code != 1 {
		t.Fatalf("selectWithFZF() code = %d, error = %v", code, err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q, want both empty", stdout.String(), stderr.String())
	}
}

func TestSelectWithFZFMissingExecutable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	code, err := Select([]Item{{Value: "alpha", Display: "alpha\talice@example.com:22"}}, "fzf", &stdout, &stderr)
	if err == nil || code != 1 {
		t.Fatalf("selectWithFZF() code = %d, error = %v", code, err)
	}
	if !strings.Contains(err.Error(), `fuzzy finder "fzf" not found`) {
		t.Fatalf("error = %q, want fzf context", err)
	}
}

func TestNormalizeFinder(t *testing.T) {
	tests := map[string]string{
		"": "", "auto": "auto", "FZF": "fzf", "sk": "sk", "skim": "sk", "peco": "peco",
	}
	for input, want := range tests {
		got, err := Normalize(input)
		if err != nil || got != want {
			t.Errorf("normalizeFinder(%q) = %q, %v; want %q, nil", input, got, err, want)
		}
	}
}

func TestFinderArguments(t *testing.T) {
	wantArguments := map[string][]string{
		"fzf":  {"--prompt=SSH host> ", "--no-multi"},
		"sk":   {"--prompt=SSH host> ", "--no-multi"},
		"peco": {"--prompt=SSH host> ", "--initial-filter=Fuzzy", "--on-cancel=error"},
	}
	for _, finder := range supportedFinders {
		want := wantArguments[finder.name]
		if strings.Join(finder.arguments, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("%s arguments = %#v, want %#v", finder.name, finder.arguments, want)
		}
	}
}

func TestSelectedValue(t *testing.T) {
	items := []Item{
		{Value: "production", Display: "production\tdeploy@prod.example.com:22"},
		{Value: "staging", Display: "staging\tubuntu@10.0.0.12:2222"},
	}
	got, err := selectedValue("staging\tubuntu@10.0.0.12:2222\n", items)
	if err != nil || got != "staging" {
		t.Fatalf("selectedValue() = %q, %v; want staging, nil", got, err)
	}
	if _, err := selectedValue("unknown\n", items); err == nil {
		t.Fatal("selectedValue() error = nil, want unknown selection error")
	}

	details := "production\tdeploy@prod.example.com:22"
	got, err = selectedValue(details+"\n", []Item{{Value: details, Display: details}})
	if err != nil || got != details {
		t.Fatalf("selectedValue() = %q, %v; want %q, nil", got, err, details)
	}
}

func TestWriteSelectedValueReportsOutputError(t *testing.T) {
	wantError := errors.New("output unavailable")
	err := writeSelectedValue(errorWriter{err: wantError}, "production")
	if !errors.Is(err, wantError) || !strings.Contains(err.Error(), "write output") {
		t.Fatalf("writeSelectedValue() error = %v, want wrapped output error", err)
	}
}

func TestResolveFinderAutomaticallyFallsBack(t *testing.T) {
	var checked []string
	lookPath := func(executable string) (string, error) {
		checked = append(checked, executable)
		if executable == "sk" {
			return "/usr/local/bin/sk", nil
		}
		return "", errors.New("not found")
	}

	finder, err := resolveFinderWith("auto", lookPath)
	if err != nil {
		t.Fatalf("resolveFinderWith() error = %v", err)
	}
	if finder.name != "sk" {
		t.Fatalf("finder = %q, want sk", finder.name)
	}
	if want := []string{"fzf", "sk"}; !reflect.DeepEqual(checked, want) {
		t.Fatalf("checked = %#v, want %#v", checked, want)
	}
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}
