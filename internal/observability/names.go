package observability

// Telemetry contract. Every metric name the application emits is one of
// these constants; sub-project 2 builds its dashboards and rules on them,
// and internal/e2e.TestEndToEndTraceAndInstrumentContract fails when one
// drifts. Keep in step with the table in OBSERVABILITY.md.
const (
	MetricHTTPServerDuration = "http.server.request.duration"
	MetricHTTPServerActive   = "http.server.active_requests"
	MetricHTTPServerRespSize = "http.server.response.body.size"
	MetricImageUploads       = "image.uploads"
	MetricImageDeletions     = "image.deletions"
	MetricCacheLookups       = "cache.lookups"
	MetricSettingsOps        = "settings.operations"
	MetricStorageOps         = "storage.operations"
	MetricStorageDuration    = "storage.operation.duration"
	MetricStorageTransferred = "storage.transferred"
	MetricMessagingSent      = "messaging.client.sent.messages"
	MetricMessagingProcess   = "messaging.process.duration"
	MetricWorkerJobs         = "worker.jobs"
	MetricQueueDepth         = "queue.depth"
	MetricQueuePending       = "queue.pending"
	MetricQueueLag           = "queue.lag"
	MetricImageProcessing    = "image.processing.duration"
	MetricDemoFaults         = "demo.faults.injected"
	MetricSpansEnded         = "telemetry.spans.ended"
	MetricSpansExported      = "telemetry.spans.exported"
)

// Attribute keys the app defines (semconv keys come from the semconv package).
const (
	AttrOutcome          = "outcome"
	AttrCacheName        = "cache.name"
	AttrCacheResult      = "cache.result"
	AttrSettingsOp       = "settings.operation"
	AttrSettingsSource   = "settings.source"
	AttrStorageProvider  = "storage.provider"
	AttrStorageOperation = "storage.operation"
	AttrStorageDirection = "storage.direction"
	AttrStorageKey       = "storage.key"
	AttrImageID          = "image.id"
	AttrImageContentType = "image.content_type"
	AttrProcessingStep   = "image.processing.step"
	AttrJobType          = "job.type"
	AttrJobAttempt       = "job.attempt"
	AttrDemoFault        = "demo.fault"
	AttrLoadgenScenario  = "loadgen.scenario"
	AttrLoadgenOp        = "loadgen.op"
)

// Outcome values used across counters.
const (
	OutcomeSuccess    = "success"
	OutcomeError      = "error"
	OutcomeFailure    = "failure"
	OutcomeRetry      = "retry"
	OutcomeDeadLetter = "dead_letter"
	OutcomeSkipped    = "skipped"
)

// ExpectedInstruments is the per-role contract asserted end to end (Go runtime
// metrics from the contrib package are asserted separately).
var ExpectedInstruments = map[string][]string{
	"web": {
		MetricHTTPServerDuration, MetricHTTPServerActive, MetricHTTPServerRespSize,
		MetricImageUploads, MetricImageDeletions, MetricCacheLookups, MetricSettingsOps,
		MetricStorageOps, MetricStorageDuration, MetricStorageTransferred,
		MetricMessagingSent, MetricDemoFaults, MetricSpansEnded, MetricSpansExported,
	},
	"worker": {
		MetricStorageOps, MetricStorageDuration, MetricStorageTransferred,
		MetricMessagingProcess, MetricWorkerJobs, MetricQueueDepth, MetricQueuePending,
		MetricQueueLag, MetricImageProcessing, MetricDemoFaults, MetricSpansEnded, MetricSpansExported,
	},
}

// RuntimeInstruments are emitted by go.opentelemetry.io/contrib/instrumentation/runtime.
var RuntimeInstruments = []string{"go.memory.used", "go.memory.limit", "go.goroutine.count"}
