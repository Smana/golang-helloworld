package queue

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestGaugesStopPollingOnceUnregistered needs no Valkey: every poll dials, and the dialer
// only counts. After Unregister, a collection (such as the final flush on shutdown, once
// the gauge client is closed) must not poll at all.
func TestGaugesStopPollingOnceUnregistered(t *testing.T) {
	var dials atomic.Int64
	rdb := redis.NewClient(&redis.Options{MaxRetries: -1, Dialer: func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		return nil, errors.New("no valkey in this test")
	}})
	t.Cleanup(func() { _ = rdb.Close() })
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	reg, err := RegisterGauges(rdb, mp.Meter("q"), DefaultStream, DefaultGroup)
	if err != nil {
		t.Fatal(err)
	}
	var rm metricdata.ResourceMetrics
	_ = reader.Collect(context.Background(), &rm) //nolint:errcheck // the poll is expected to fail
	if dials.Load() == 0 {
		t.Fatal("a collection did not poll: the test cannot tell polling from not polling")
	}

	if err := reg.Unregister(); err != nil {
		t.Fatal(err)
	}
	before := dials.Load()
	_ = reader.Collect(context.Background(), &rm) //nolint:errcheck // nothing is registered any more
	if got := dials.Load(); got != before {
		t.Errorf("a collection after Unregister still polled (%d dials)", got-before)
	}
}
