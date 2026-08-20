package finder

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	fuzzyfinder "github.com/ktr0731/go-fuzzyfinder"
)

func TestSelectWithNoHosts(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	find := func(interface{}, func(int) string, ...fuzzyfinder.Option) (int, error) {
		called = true
		return 0, nil
	}

	code, err := selectWithFind(nil, &stdout, &stderr, find)
	if err != nil || code != 1 {
		t.Fatalf("selectWithFind() code = %d, error = %v", code, err)
	}
	if called {
		t.Fatal("selectWithFind() called finder for empty items")
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q, want both empty", stdout.String(), stderr.String())
	}
}

func TestSelectUsesDisplayAndWritesValue(t *testing.T) {
	items := []Item{
		{Value: "production", Display: "production\tdeploy@prod.example.com:22"},
		{Value: "staging", Display: "staging\tubuntu@10.0.0.12:2222"},
	}
	var stdout, stderr bytes.Buffer
	var gotItems interface{}
	var gotDisplay []string
	var gotOptions int
	find := func(slice interface{}, itemFunc func(int) string, options ...fuzzyfinder.Option) (int, error) {
		gotItems = slice
		gotDisplay = []string{itemFunc(0), itemFunc(1)}
		gotOptions = len(options)
		return 1, nil
	}

	code, err := selectWithFind(items, &stdout, &stderr, find)
	if err != nil || code != 0 {
		t.Fatalf("selectWithFind() code = %d, error = %v", code, err)
	}
	if !reflect.DeepEqual(gotItems, items) {
		t.Fatalf("items = %#v, want %#v", gotItems, items)
	}
	if gotDisplay[0] != items[0].Display || gotDisplay[1] != items[1].Display {
		t.Fatalf("display = %#v, want %#v", gotDisplay, []string{items[0].Display, items[1].Display})
	}
	if gotOptions != 1 {
		t.Fatalf("options = %d, want prompt option", gotOptions)
	}
	if stdout.String() != "staging\n" {
		t.Fatalf("stdout = %q, want selected value", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestSelectPreservesDetailedValue(t *testing.T) {
	details := "production\tdeploy@prod.example.com:22"
	items := []Item{{Value: details, Display: details}}
	var stdout bytes.Buffer
	find := func(interface{}, func(int) string, ...fuzzyfinder.Option) (int, error) {
		return 0, nil
	}

	code, err := selectWithFind(items, &stdout, io.Discard, find)
	if err != nil || code != 0 {
		t.Fatalf("selectWithFind() code = %d, error = %v", code, err)
	}
	if stdout.String() != details+"\n" {
		t.Fatalf("stdout = %q, want detailed selection", stdout.String())
	}
}

func TestSelectCancellationHasNoError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	find := func(interface{}, func(int) string, ...fuzzyfinder.Option) (int, error) {
		return 0, fuzzyfinder.ErrAbort
	}

	code, err := selectWithFind([]Item{{Value: "alpha", Display: "alpha\talice@example.com:22"}}, &stdout, &stderr, find)
	if err != nil {
		t.Fatalf("selectWithFind() error = %v, want nil on cancellation", err)
	}
	if code == 0 {
		t.Fatal("selectWithFind() code = 0, want non-zero on cancellation")
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q, want both empty", stdout.String(), stderr.String())
	}
}

func TestSelectWrapsFinderError(t *testing.T) {
	wantError := errors.New("terminal unavailable")
	find := func(interface{}, func(int) string, ...fuzzyfinder.Option) (int, error) {
		return 0, wantError
	}

	code, err := selectWithFind([]Item{{Value: "alpha", Display: "alpha"}}, io.Discard, io.Discard, find)
	if code != 1 || !errors.Is(err, wantError) || !strings.Contains(err.Error(), "select host") {
		t.Fatalf("selectWithFind() code = %d, error = %v, want wrapped finder error", code, err)
	}
}

func TestSelectRejectsInvalidIndex(t *testing.T) {
	find := func(interface{}, func(int) string, ...fuzzyfinder.Option) (int, error) {
		return 2, nil
	}

	code, err := selectWithFind([]Item{{Value: "alpha", Display: "alpha"}}, io.Discard, io.Discard, find)
	if code != 1 || err == nil || !strings.Contains(err.Error(), "invalid selection index") {
		t.Fatalf("selectWithFind() code = %d, error = %v, want invalid-index error", code, err)
	}
}

func TestWriteSelectedValueReportsOutputError(t *testing.T) {
	wantError := errors.New("output unavailable")
	err := writeSelectedValue(errorWriter{err: wantError}, "production")
	if !errors.Is(err, wantError) || !strings.Contains(err.Error(), "write output") {
		t.Fatalf("writeSelectedValue() error = %v, want wrapped output error", err)
	}
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}
