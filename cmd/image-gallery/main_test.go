package main

import (
	"testing"

	"image-gallery/internal/observability"
)

func TestRunReportsTheBuiltVersion(t *testing.T) {
	defer func(v, d string) { version, observability.DefaultServiceVersion = v, d }(version, observability.DefaultServiceVersion)
	version = "9.9.9-built"
	run([]string{"help"})
	if observability.DefaultServiceVersion != "9.9.9-built" {
		t.Errorf("default service version = %q, want main.version %q", observability.DefaultServiceVersion, "9.9.9-built")
	}
}

func TestRunDispatch(t *testing.T) {
	if code := run([]string{"help"}); code != 0 {
		t.Errorf("help exit = %d, want 0", code)
	}
	if code := run([]string{"no-such-command"}); code != 2 {
		t.Errorf("unknown command exit = %d, want 2", code)
	}
	if code := run([]string{"loadgen", "--help"}); code != 0 {
		t.Errorf("loadgen --help exit = %d, want 0", code)
	}
	if code := run([]string{"loadgen"}); code != 2 {
		t.Errorf("loadgen without --target exit = %d, want 2", code)
	}
}
