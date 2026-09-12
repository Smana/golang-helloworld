package queue

import (
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestJobRoundTripAndPermanent(t *testing.T) {
	j := Job{Type: JobTypeProcessImage, ImageID: 42, ObjectKey: "aa/bb/x.png",
		Carrier: map[string]string{"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}} // pragma: allowlist secret
	vals := map[string]any{}
	for k, v := range j.values() {
		vals[k] = v
	}
	got, err := jobFromMessage(redis.XMessage{ID: "1-0", Values: vals})
	if err != nil || got.ImageID != 42 || got.ObjectKey != j.ObjectKey || got.Carrier["traceparent"] != j.Carrier["traceparent"] || got.ID != "1-0" {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	if _, err := jobFromMessage(redis.XMessage{ID: "2-0", Values: map[string]any{"image_id": "x"}}); !IsPermanent(err) {
		t.Fatalf("a malformed image_id must be permanent, got %v", err)
	}
	if IsPermanent(errors.New("plain")) || !IsPermanent(Permanent(errors.New("p"))) {
		t.Fatal("IsPermanent")
	}
}

func TestEntryAge(t *testing.T) {
	now := time.UnixMilli(10_000)
	if got := entryAge("7000-3", now); got != 3 {
		t.Fatalf("entryAge = %v, want 3", got)
	}
	if got := entryAge("garbage", now); got != 0 {
		t.Fatalf("entryAge(garbage) = %v, want 0", got)
	}
}
