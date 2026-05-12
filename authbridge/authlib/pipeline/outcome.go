package pipeline

import "time"

// OutcomeAction classifies the terminal state of a request. Intentionally
// a small 3-value vocabulary distinct from the 5-value InvocationAction:
// Outcome describes the request as a whole (for OnFinish accounting),
// while InvocationAction describes what one plugin did in one phase.
//
// A rate-limiter refunding a slot cares about "was this call
// successful" (OutcomeAllow) vs "did a plugin choose to deny"
// (OutcomeDeny) vs "did the upstream / framework fail" (OutcomeError)
// — three distinct accounting buckets. The 5-value InvocationAction
// (allow / deny / skip / modify / observe) doesn't answer that
// question because skip / modify / observe are mid-pipeline, not
// terminal states.
type OutcomeAction string

const (
	// OutcomeAllow — every plugin returned Continue; response was
	// produced and sent to the client.
	OutcomeAllow OutcomeAction = "allow"

	// OutcomeDeny — a plugin (request-side or response-side) returned
	// Reject. Outcome.DenyingPlugin names it.
	OutcomeDeny OutcomeAction = "deny"

	// OutcomeError — the request terminated without a plugin denial:
	// upstream transport failure, context cancellation, a panic
	// recovered inside the dispatcher. DenyingPlugin is empty.
	OutcomeError OutcomeAction = "error"
)

// Outcome carries the terminal state of a request — what final action
// the pipeline took, the resulting HTTP status, which plugin denied (if
// any), and how long the request took end-to-end. Populated by the
// framework exactly once per request, immediately before dispatching
// OnFinish on any Finisher-implementing plugins.
//
// Read via pctx.Outcome(). The getter returns nil during OnRequest and
// OnResponse — plugins that accidentally inspect Outcome in those
// phases observe a nil pointer rather than a stale zero value, so the
// "this field is only meaningful in OnFinish" contract is enforced at
// read time rather than documented and forgotten.
//
// Outcome is deliberately a small struct. If a future need demands
// more context (upstream response headers, body sha256, external
// request ID, etc.) add a field here rather than inventing a parallel
// mechanism — plugins already reach for pctx.Outcome().
type Outcome struct {
	// FinalAction classifies the request as Allow / Deny / Error —
	// three accounting buckets useful for per-outcome metrics and
	// stateful-plugin cleanup.
	FinalAction OutcomeAction

	// StatusCode is the final HTTP status written to the downstream
	// client. Zero for errors that never produced a response.
	StatusCode int

	// DenyingPlugin names the plugin whose Reject action stopped the
	// pipeline. Empty when FinalAction != OutcomeDeny.
	DenyingPlugin string

	// Duration is wall-clock time between pctx.StartedAt and the
	// moment OnFinish dispatches (after the response is on the wire).
	// Useful for per-outcome latency accounting.
	Duration time.Duration
}

// Outcome returns the terminal outcome of the request. Valid only
// during OnFinish — returns nil in OnRequest and OnResponse. Plugins
// implementing Finisher can rely on the return being non-nil; there is
// no path in the dispatcher that calls OnFinish with an unpopulated
// outcome.
func (c *Context) Outcome() *Outcome {
	return c.outcome
}
