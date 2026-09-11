package testutils

import "testing"

// TestSecureRandInt63n_NeverNegative guards against a regression where converting
// a random uint64 to int64 could yield a negative value that Go's `%` operator
// then preserves (Go's modulo keeps the sign of the dividend), producing
// negative "random" values out of the requested [0, n) range. Callers such as
// CreateTestImage rely on this staying non-negative to satisfy DB CHECK
// constraints (e.g. images.file_size >= 0).
func TestSecureRandInt63n_NeverNegative(t *testing.T) {
	const n = int64(99)
	for i := 0; i < 10000; i++ {
		v := secureRandInt63n(n)
		if v < 0 || v >= n {
			t.Fatalf("secureRandInt63n(%d) = %d, want value in [0, %d)", n, v, n)
		}
	}
}

// TestSecureRandIntn_NeverNegative exercises the int-returning wrapper used for
// width/height randomization, which must also stay within [0, n).
func TestSecureRandIntn_NeverNegative(t *testing.T) {
	const n = 400
	for i := 0; i < 10000; i++ {
		v := secureRandIntn(n)
		if v < 0 || v >= n {
			t.Fatalf("secureRandIntn(%d) = %d, want value in [0, %d)", n, v, n)
		}
	}
}
