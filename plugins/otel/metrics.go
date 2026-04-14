package otel

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// MetricsConfig holds configuration for the OTEL metrics exporter
type MetricsConfig struct {
	ServiceName  string
	Endpoint     string
	Headers      map[string]string
	Protocol     Protocol
	TLSCACert    string
	Insecure     bool // Skip TLS when true; ignored if TLSCACert is set
	PushInterval int  // in seconds
}

// MetricsExporter handles OTEL metrics export
type MetricsExporter struct {
	provider *sdkmetric.MeterProvider
	meter    metric.Meter

	// Bifrost metrics - counters
	upstreamRequestsTotal *syncInt64Counter
	successRequestsTotal  *syncInt64Counter
	errorRequestsTotal    *syncInt64Counter
	inputTokensTotal      *syncInt64Counter
	outputTokensTotal     *syncInt64Counter
	cacheHitsTotal        *syncInt64Counter

	// Bifrost metrics - float counters (for cost)
	costTotal *syncFloat64Counter

	// Bifrost metrics - histograms
	upstreamLatencySeconds         *syncFloat64Histogram
	streamFirstTokenLatencySeconds *syncFloat64Histogram
	streamInterTokenLatencySeconds *syncFloat64Histogram

	// HTTP metrics
	httpRequestsTotal     *syncInt64Counter
	httpRequestDuration   *syncFloat64Histogram
	httpRequestSizeBytes  *syncFloat64Histogram
	httpResponseSizeBytes *syncFloat64Histogram
}

// onceCounter provides thread-safe once-initialization for any OTel metric instrument.
type onceCounter[I any] struct {
	counter I
	ok      bool
	once    sync.Once
}

func (o *onceCounter[I]) load(name string, create func() (I, error)) (I, bool) {
	o.once.Do(func() {
		var err error
		o.counter, err = create()
		o.ok = err == nil
		if err != nil {
			logger.Error("failed to create metric %s: %v", name, err)
		}
	})
	return o.counter, o.ok
}

// syncInt64Counter wraps metric.Int64Counter with thread-safe lazy initialization
type syncInt64Counter struct {
	onceCounter[metric.Int64Counter]
	name, desc, unit string
	meter            metric.Meter
}

func newSyncInt64Counter(name, desc, unit string, meter metric.Meter) *syncInt64Counter {
	return &syncInt64Counter{name: name, desc: desc, unit: unit, meter: meter}
}

func (c *syncInt64Counter) Add(ctx context.Context, value int64, opts ...metric.AddOption) {
	if inst, ok := c.load(c.name, func() (metric.Int64Counter, error) {
		return c.meter.Int64Counter(c.name, metric.WithDescription(c.desc), metric.WithUnit(c.unit))
	}); ok {
		inst.Add(ctx, value, opts...)
	}
}

// syncFloat64Counter wraps metric.Float64Counter with thread-safe lazy initialization
type syncFloat64Counter struct {
	onceCounter[metric.Float64Counter]
	name, desc, unit string
	meter            metric.Meter
}

func newSyncFloat64Counter(name, desc, unit string, meter metric.Meter) *syncFloat64Counter {
	return &syncFloat64Counter{name: name, desc: desc, unit: unit, meter: meter}
}

func (c *syncFloat64Counter) Add(ctx context.Context, value float64, opts ...metric.AddOption) {
	if inst, ok := c.load(c.name, func() (metric.Float64Counter, error) {
		return c.meter.Float64Counter(c.name, metric.WithDescription(c.desc), metric.WithUnit(c.unit))
	}); ok {
		inst.Add(ctx, value, opts...)
	}
}

// syncFloat64Histogram wraps metric.Float64Histogram with thread-safe lazy initialization
type syncFloat64Histogram struct {
	onceCounter[metric.Float64Histogram]
	name, desc, unit string
	meter            metric.Meter
}

func newSyncFloat64Histogram(name, desc, unit string, meter metric.Meter) *syncFloat64Histogram {
	return &syncFloat64Histogram{name: name, desc: desc, unit: unit, meter: meter}
}

func (h *syncFloat64Histogram) Record(ctx context.Context, value float64, opts ...metric.RecordOption) {
	if inst, ok := h.load(h.name, func() (metric.Float64Histogram, error) {
		return h.meter.Float64Histogram(h.name, metric.WithDescription(h.desc), metric.WithUnit(h.unit))
	}); ok {
		inst.Record(ctx, value, opts...)
	}
}

// NewMetricsExporter creates a new OTEL metrics exporter
func NewMetricsExporter(ctx context.Context, config *MetricsConfig, version string) (*MetricsExporter, error) {
	// Generate a unique instance ID for this node
	instanceID, err := os.Hostname()
	if err != nil {
		instanceID = fmt.Sprintf("bifrost-%d", time.Now().UnixNano())
	}

	// Create resource with service info
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(config.ServiceName),
			semconv.ServiceInstanceID(instanceID),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create exporter based on protocol
	var exporter sdkmetric.Exporter
	if config.Protocol == ProtocolGRPC {
		exporter, err = createGRPCExporter(ctx, config)
	} else {
		exporter, err = createHTTPExporter(ctx, config)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create exporter: %w", err)
	}

	// Create meter provider with periodic reader
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				exporter,
				sdkmetric.WithInterval(time.Duration(config.PushInterval)*time.Second),
			),
		),
	)

	// Set as global provider
	otel.SetMeterProvider(provider)

	// Create meter
	meter := provider.Meter("bifrost",
		metric.WithInstrumentationVersion(version),
	)

	// Create metrics exporter
	m := &MetricsExporter{
		provider: provider,
		meter:    meter,
	}

	// Initialize metrics with lazy loading wrappers
	m.initMetrics()

	return m, nil
}

func createHTTPExporter(ctx context.Context, config *MetricsConfig) (sdkmetric.Exporter, error) {
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpointURL(config.Endpoint),
	}

	if len(config.Headers) > 0 {
		opts = append(opts, otlpmetrichttp.WithHeaders(config.Headers))
	}

	// HTTP metrics insecure mode disables TLS entirely (unlike the trace HTTP client
	// which uses InsecureSkipVerify). buildTLSConfig is bypassed for that case.
	if config.TLSCACert == "" && config.Insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	} else {
		tlsConfig, err := buildTLSConfig(config.TLSCACert, false)
		if err != nil {
			return nil, err
		}
		opts = append(opts, otlpmetrichttp.WithTLSClientConfig(tlsConfig))
	}

	return otlpmetrichttp.New(ctx, opts...)
}

func createGRPCExporter(ctx context.Context, config *MetricsConfig) (sdkmetric.Exporter, error) {
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(config.Endpoint),
	}

	if len(config.Headers) > 0 {
		opts = append(opts, otlpmetricgrpc.WithHeaders(config.Headers))
	}

	// gRPC insecure mode uses plaintext (no TLS at all). buildTLSConfig is bypassed for that case.
	if config.TLSCACert == "" && config.Insecure {
		opts = append(opts, otlpmetricgrpc.WithTLSCredentials(insecure.NewCredentials()))
	} else {
		tlsConfig, err := buildTLSConfig(config.TLSCACert, false)
		if err != nil {
			return nil, err
		}
		opts = append(opts, otlpmetricgrpc.WithTLSCredentials(credentials.NewTLS(tlsConfig)))
	}

	return otlpmetricgrpc.New(ctx, opts...)
}

func (m *MetricsExporter) initMetrics() {
	for _, s := range []struct {
		name, desc, unit string
		ptr              **syncInt64Counter
	}{
		{"bifrost_upstream_requests_total", "Total number of requests forwarded to upstream providers by Bifrost", "{request}", &m.upstreamRequestsTotal},
		{"bifrost_success_requests_total", "Total number of successful requests forwarded to upstream providers by Bifrost", "{request}", &m.successRequestsTotal},
		{"bifrost_error_requests_total", "Total number of error requests forwarded to upstream providers by Bifrost", "{request}", &m.errorRequestsTotal},
		{"bifrost_input_tokens_total", "Total number of input tokens forwarded to upstream providers by Bifrost", "{token}", &m.inputTokensTotal},
		{"bifrost_output_tokens_total", "Total number of output tokens forwarded to upstream providers by Bifrost", "{token}", &m.outputTokensTotal},
		{"bifrost_cache_hits_total", "Total number of cache hits forwarded to upstream providers by Bifrost", "{hit}", &m.cacheHitsTotal},
		{"http_requests_total", "Total number of HTTP requests", "{request}", &m.httpRequestsTotal},
	} {
		*s.ptr = newSyncInt64Counter(s.name, s.desc, s.unit, m.meter)
	}

	m.costTotal = newSyncFloat64Counter("bifrost_cost_total", "Total cost in USD for requests to upstream providers", "USD", m.meter)

	for _, s := range []struct {
		name, desc, unit string
		ptr              **syncFloat64Histogram
	}{
		{"bifrost_upstream_latency_seconds", "Latency of requests forwarded to upstream providers by Bifrost", "s", &m.upstreamLatencySeconds},
		{"bifrost_stream_first_token_latency_seconds", "Latency of the first token of a stream response", "s", &m.streamFirstTokenLatencySeconds},
		{"bifrost_stream_inter_token_latency_seconds", "Latency of the intermediate tokens of a stream response", "s", &m.streamInterTokenLatencySeconds},
		{"http_request_duration_seconds", "Duration of HTTP requests", "s", &m.httpRequestDuration},
		{"http_request_size_bytes", "Size of HTTP requests", "By", &m.httpRequestSizeBytes},
		{"http_response_size_bytes", "Size of HTTP responses", "By", &m.httpResponseSizeBytes},
	} {
		*s.ptr = newSyncFloat64Histogram(s.name, s.desc, s.unit, m.meter)
	}
}

// Shutdown gracefully shuts down the metrics exporter
func (m *MetricsExporter) Shutdown(ctx context.Context) error {
	if m.provider != nil {
		return m.provider.Shutdown(ctx)
	}
	return nil
}

// RecordUpstreamRequest records an upstream request metric
func (m *MetricsExporter) RecordUpstreamRequest(ctx context.Context, attrs ...attribute.KeyValue) {
	m.upstreamRequestsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordSuccessRequest records a successful request metric
func (m *MetricsExporter) RecordSuccessRequest(ctx context.Context, attrs ...attribute.KeyValue) {
	m.successRequestsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordErrorRequest records an error request metric
func (m *MetricsExporter) RecordErrorRequest(ctx context.Context, attrs ...attribute.KeyValue) {
	m.errorRequestsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordInputTokens records input tokens metric
func (m *MetricsExporter) RecordInputTokens(ctx context.Context, count int64, attrs ...attribute.KeyValue) {
	m.inputTokensTotal.Add(ctx, count, metric.WithAttributes(attrs...))
}

// RecordOutputTokens records output tokens metric
func (m *MetricsExporter) RecordOutputTokens(ctx context.Context, count int64, attrs ...attribute.KeyValue) {
	m.outputTokensTotal.Add(ctx, count, metric.WithAttributes(attrs...))
}

// RecordCacheHit records a cache hit metric
func (m *MetricsExporter) RecordCacheHit(ctx context.Context, attrs ...attribute.KeyValue) {
	m.cacheHitsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordCost records cost metric
func (m *MetricsExporter) RecordCost(ctx context.Context, cost float64, attrs ...attribute.KeyValue) {
	m.costTotal.Add(ctx, cost, metric.WithAttributes(attrs...))
}

// RecordUpstreamLatency records upstream latency metric
func (m *MetricsExporter) RecordUpstreamLatency(ctx context.Context, latencySeconds float64, attrs ...attribute.KeyValue) {
	m.upstreamLatencySeconds.Record(ctx, latencySeconds, metric.WithAttributes(attrs...))
}

// RecordStreamFirstTokenLatency records first token latency metric
func (m *MetricsExporter) RecordStreamFirstTokenLatency(ctx context.Context, latencySeconds float64, attrs ...attribute.KeyValue) {
	m.streamFirstTokenLatencySeconds.Record(ctx, latencySeconds, metric.WithAttributes(attrs...))
}

// RecordStreamInterTokenLatency records inter-token latency metric
func (m *MetricsExporter) RecordStreamInterTokenLatency(ctx context.Context, latencySeconds float64, attrs ...attribute.KeyValue) {
	m.streamInterTokenLatencySeconds.Record(ctx, latencySeconds, metric.WithAttributes(attrs...))
}

// RecordHTTPRequest records an HTTP request metric
func (m *MetricsExporter) RecordHTTPRequest(ctx context.Context, attrs ...attribute.KeyValue) {
	m.httpRequestsTotal.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// RecordHTTPRequestDuration records HTTP request duration metric
func (m *MetricsExporter) RecordHTTPRequestDuration(ctx context.Context, durationSeconds float64, attrs ...attribute.KeyValue) {
	m.httpRequestDuration.Record(ctx, durationSeconds, metric.WithAttributes(attrs...))
}

// RecordHTTPRequestSize records HTTP request size metric
func (m *MetricsExporter) RecordHTTPRequestSize(ctx context.Context, sizeBytes float64, attrs ...attribute.KeyValue) {
	m.httpRequestSizeBytes.Record(ctx, sizeBytes, metric.WithAttributes(attrs...))
}

// RecordHTTPResponseSize records HTTP response size metric
func (m *MetricsExporter) RecordHTTPResponseSize(ctx context.Context, sizeBytes float64, attrs ...attribute.KeyValue) {
	m.httpResponseSizeBytes.Record(ctx, sizeBytes, metric.WithAttributes(attrs...))
}

// BuildBifrostAttributes builds common Bifrost metric attributes
func BuildBifrostAttributes(provider, model, method, virtualKeyID, virtualKeyName, selectedKeyID, selectedKeyName string, numberOfRetries, fallbackIndex int, teamID, teamName, customerID, customerName string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("provider", provider),
		attribute.String("model", model),
		attribute.String("method", method),
		attribute.String("virtual_key_id", virtualKeyID),
		attribute.String("virtual_key_name", virtualKeyName),
		attribute.String("selected_key_id", selectedKeyID),
		attribute.String("selected_key_name", selectedKeyName),
		attribute.Int("number_of_retries", numberOfRetries),
		attribute.Int("fallback_index", fallbackIndex),
		attribute.String("team_id", teamID),
		attribute.String("team_name", teamName),
		attribute.String("customer_id", customerID),
		attribute.String("customer_name", customerName),
	}
}

// BuildHTTPAttributes builds common HTTP metric attributes
func BuildHTTPAttributes(path, method, status string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("path", path),
		attribute.String("method", method),
		attribute.String("status", status),
	}
}

// Helper functions for type-safe attribute extraction from trace spans
func getStringAttr(attrs map[string]any, key string) string {
	if attrs == nil {
		return ""
	}
	if v, ok := attrs[key].(string); ok {
		return v
	}
	return ""
}

func getIntAttr(attrs map[string]any, key string) int {
	if attrs == nil {
		return 0
	}
	switch v := attrs[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

func getFloat64Attr(attrs map[string]any, key string) float64 {
	if attrs == nil {
		return 0
	}
	switch v := attrs[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

// buildSpanAttrs extracts metric dimension attrs from a single attempt span.
func buildSpanAttrs(span *schemas.Span) []attribute.KeyValue {
	attrs := span.Attributes
	method := getStringAttr(attrs, "request.type")
	if method == "" {
		method = span.Name
	}
	return BuildBifrostAttributes(
		getStringAttr(attrs, schemas.AttrProviderName),
		getStringAttr(attrs, schemas.AttrRequestModel),
		method,
		getStringAttr(attrs, schemas.AttrVirtualKeyID),
		getStringAttr(attrs, schemas.AttrVirtualKeyName),
		getStringAttr(attrs, schemas.AttrSelectedKeyID),
		getStringAttr(attrs, schemas.AttrSelectedKeyName),
		getIntAttr(attrs, schemas.AttrNumberOfRetries),
		getIntAttr(attrs, schemas.AttrFallbackIndex),
		getStringAttr(attrs, schemas.AttrTeamID),
		getStringAttr(attrs, schemas.AttrTeamName),
		getStringAttr(attrs, schemas.AttrCustomerID),
		getStringAttr(attrs, schemas.AttrCustomerName),
	)
}

// recordMetricsFromTrace extracts metrics data from a completed trace and records them
// via the OTEL metrics exporter. This is called from Inject after trace emission.
//
// Per-attempt metrics (upstream_requests, errors, success, latency) are recorded once
// per llm.call/retry span so fallback attempts and failed retries are counted with
// their own provider/model/fallback_index labels. Per-trace metrics (tokens, cost,
// TTFT) are recorded once, keyed off the final (latest) attempt span.
func (m *MetricsExporter) recordMetricsFromTrace(ctx context.Context, trace *schemas.Trace) {
	if trace == nil || m == nil {
		return
	}

	var finalSpan *schemas.Span
	for _, span := range trace.Spans {
		if span.Kind != schemas.SpanKindLLMCall && span.Kind != schemas.SpanKindRetry {
			continue
		}

		spanAttrs := buildSpanAttrs(span)

		m.RecordUpstreamRequest(ctx, spanAttrs...)

		if !span.StartTime.IsZero() && !span.EndTime.IsZero() {
			latencySeconds := span.EndTime.Sub(span.StartTime).Seconds()
			m.RecordUpstreamLatency(ctx, latencySeconds, spanAttrs...)
		}

		if span.Status == schemas.SpanStatusError {
			m.RecordErrorRequest(ctx, spanAttrs...)
		} else {
			m.RecordSuccessRequest(ctx, spanAttrs...)
		}

		if finalSpan == nil || span.EndTime.After(finalSpan.EndTime) {
			finalSpan = span
		}
	}

	if finalSpan == nil {
		finalSpan = trace.RootSpan
	}
	if finalSpan == nil {
		return
	}

	attrs := finalSpan.Attributes
	otelAttrs := buildSpanAttrs(finalSpan)

	// Record token usage - try both naming conventions
	inputTokens := getIntAttr(attrs, schemas.AttrPromptTokens)
	if inputTokens == 0 {
		inputTokens = getIntAttr(attrs, schemas.AttrInputTokens)
	}
	if inputTokens > 0 {
		m.RecordInputTokens(ctx, int64(inputTokens), otelAttrs...)
	}

	outputTokens := getIntAttr(attrs, schemas.AttrCompletionTokens)
	if outputTokens == 0 {
		outputTokens = getIntAttr(attrs, schemas.AttrOutputTokens)
	}
	if outputTokens > 0 {
		m.RecordOutputTokens(ctx, int64(outputTokens), otelAttrs...)
	}

	// Record cost if available
	cost := getFloat64Attr(attrs, schemas.AttrUsageCost)
	if cost > 0 {
		m.RecordCost(ctx, cost, otelAttrs...)
	}

	// Record streaming latency metrics if available
	ttft := getFloat64Attr(attrs, schemas.AttrTimeToFirstToken)
	if ttft > 0 {
		// Convert from nanoseconds to seconds if needed (check the unit)
		m.RecordStreamFirstTokenLatency(ctx, ttft/1e9, otelAttrs...)
	}
}
