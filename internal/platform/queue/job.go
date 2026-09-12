// Package queue is a job queue on a Valkey stream with a consumer group:
// trace context rides in each entry, failures retry with exponential backoff
// and then dead-letter, and XAUTOCLAIM recovers entries a dead worker held.
package queue

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Stream, group and job-type names (the spec's contract).
const (
	DefaultStream       = "image-gallery:jobs"
	DefaultGroup        = "workers"
	DefaultDeadLetter   = "image-gallery:jobs:dead"
	JobTypeProcessImage = "process_image"
	messagingSystem     = "valkey"
)

// ErrSkip tells the consumer the job needs no work (already done): ack it,
// count it as skipped, and do not retry.
var ErrSkip = errors.New("job skipped")

// Job is one stream entry.
type Job struct {
	ID        string // stream entry ID; set by the consumer
	Type      string
	ImageID   int
	ObjectKey string
	Carrier   map[string]string // W3C traceparent/tracestate of the producer span
}

func (j Job) values() map[string]any {
	v := map[string]any{"type": j.Type, "image_id": strconv.Itoa(j.ImageID), "object_key": j.ObjectKey}
	for k, val := range j.Carrier {
		v[k] = val
	}
	return v
}

func jobFromMessage(m redis.XMessage) (Job, error) {
	get := func(k string) string {
		if s, ok := m.Values[k].(string); ok {
			return s
		}
		return ""
	}
	carrier := map[string]string{}
	for _, k := range []string{"traceparent", "tracestate"} {
		if v := get(k); v != "" {
			carrier[k] = v
		}
	}
	job := Job{ID: m.ID, Type: get("type"), ObjectKey: get("object_key"), Carrier: carrier}
	id, err := strconv.Atoi(get("image_id"))
	if err != nil {
		return job, Permanent(fmt.Errorf("malformed image_id %q: %w", get("image_id"), err))
	}
	job.ImageID = id
	return job, nil
}

type permanentError struct{ err error }

func (p permanentError) Error() string { return p.err.Error() }
func (p permanentError) Unwrap() error { return p.err }

// Permanent marks a failure retrying cannot fix: dead-letter immediately.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsPermanent reports whether err (or anything it wraps) is Permanent.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

// entryAge is now minus the millisecond timestamp encoded in a stream ID ("<ms>-<seq>").
func entryAge(id string, now time.Time) float64 {
	ms, err := strconv.ParseInt(strings.SplitN(id, "-", 2)[0], 10, 64)
	if err != nil {
		return 0
	}
	return now.Sub(time.UnixMilli(ms)).Seconds()
}
