package finder

import (
	"errors"
	"fmt"
	"io"

	fuzzyfinder "github.com/ktr0731/go-fuzzyfinder"
)

// Item separates the text shown in the finder from the value returned after
// selection.
type Item struct {
	Value   string
	Display string
}

// findFunc is the part of go-fuzzyfinder used by Select. Keeping it as a
// function type lets tests exercise the selection/output boundary without
// requiring a real terminal.
type findFunc func(interface{}, func(int) string, ...fuzzyfinder.Option) (int, error)

// Select runs the built-in fuzzy finder with items as input and writes the
// selected item's value. A cancelled selection returns a non-zero status and
// a nil error so callers do not report cancellation as a failure.
func Select(items []Item, stdout, stderr io.Writer) (int, error) {
	return selectWithFind(items, stdout, stderr, fuzzyfinder.Find)
}

func selectWithFind(items []Item, stdout, _ io.Writer, find findFunc) (int, error) {
	if len(items) == 0 {
		return 1, nil
	}

	selected, err := find(
		items,
		func(i int) string {
			return items[i].Display
		},
		fuzzyfinder.WithPromptString("SSH host> "),
	)
	if errors.Is(err, fuzzyfinder.ErrAbort) {
		return 1, nil
	}
	if err != nil {
		return 1, fmt.Errorf("select host: %w", err)
	}
	if selected < 0 || selected >= len(items) {
		return 1, fmt.Errorf("fuzzy finder returned invalid selection index %d", selected)
	}
	if err := writeSelectedValue(stdout, items[selected].Value); err != nil {
		return 1, err
	}
	return 0, nil
}

func writeSelectedValue(output io.Writer, value string) error {
	if _, err := fmt.Fprintln(output, value); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
