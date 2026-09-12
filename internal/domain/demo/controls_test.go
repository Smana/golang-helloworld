package demo

import (
	"errors"
	"testing"
)

func TestValidateBoundsWorkerDelay(t *testing.T) {
	if err := (Controls{WorkerDelayMS: 10000}).Validate(); err != nil {
		t.Errorf("10000 ms rejected: %v", err)
	}
	if err := (Controls{WorkerDelayMS: 10001}).Validate(); !errors.Is(err, ErrInvalidControls) {
		t.Errorf("10001 ms: err = %v, want ErrInvalidControls", err)
	}
}
