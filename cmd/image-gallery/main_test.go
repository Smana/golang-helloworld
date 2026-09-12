package main

import "testing"

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
