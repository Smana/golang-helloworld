package queue

import (
	"testing"
	"time"

	"image-gallery/internal/domain/demo"
)

// A job delayed by the demo on every attempt must finish before XAutoClaim
// treats it as abandoned, or a second consumer processes it concurrently.
func TestMaxWorkerDelayFitsInsideClaimIdle(t *testing.T) {
	o := defaultOptions()
	worst := time.Duration(o.maxAttempts) * demo.MaxWorkerDelayMS * time.Millisecond
	for n := 1; n < o.maxAttempts; n++ {
		worst += o.backoff(n)
	}
	if worst >= o.claimIdle {
		t.Fatalf("%d attempts at demo.MaxWorkerDelayMS plus backoff take %v, not under claimIdle %v",
			o.maxAttempts, worst, o.claimIdle)
	}
}
