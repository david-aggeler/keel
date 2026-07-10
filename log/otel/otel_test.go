package otel_test

import (
	"context"
	"log/slog"
	"testing"

	logging "github.com/david-aggeler/keel/log"
	logotel "github.com/david-aggeler/keel/log/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type captureExporter struct {
	records []sdklog.Record
}

func (e *captureExporter) Export(_ context.Context, records []sdklog.Record) error {
	for _, record := range records {
		e.records = append(e.records, record.Clone())
	}
	return nil
}

func (e *captureExporter) ForceFlush(context.Context) error { return nil }

func (e *captureExporter) Shutdown(context.Context) error { return nil }

// DHF-TEST: keel/requirement-22
func TestHandlerExportsResourceAndActiveSpanCorrelation(t *testing.T) {
	ctx := context.Background()
	exporter := &captureExporter{}
	handler, shutdown, err := logotel.NewHandler(ctx, logotel.Config{
		ServiceName: "svc",
		Exporter:    exporter,
		Resource: resource.NewWithAttributes(
			"",
			attribute.String("deployment.environment", "test"),
		),
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	t.Cleanup(func() {
		if err := shutdown(context.Background()); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	})

	tracerProvider := sdktrace.NewTracerProvider()
	defer func() { _ = tracerProvider.Shutdown(context.Background()) }()
	otel.SetTracerProvider(tracerProvider)
	spanCtx, span := tracerProvider.Tracer("keel/log/otel-test").Start(ctx, "operation")
	defer span.End()

	logger := logging.New(logging.Config{
		Service:  "svc",
		Console:  logging.ConsoleNone,
		Handlers: []slog.Handler{handler},
	})
	logger.InfoContext(spanCtx, "sent", "answer", 42)

	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown flush: %v", err)
	}
	if len(exporter.records) != 1 {
		t.Fatalf("exported records = %d, want 1", len(exporter.records))
	}
	record := exporter.records[0]
	if got := record.TraceID(); got != span.SpanContext().TraceID() {
		t.Fatalf("trace_id = %s, want %s", got, span.SpanContext().TraceID())
	}
	if got := record.SpanID(); got != span.SpanContext().SpanID() {
		t.Fatalf("span_id = %s, want %s", got, span.SpanContext().SpanID())
	}
	serviceName, ok := record.Resource().Set().Value(attribute.Key("service.name"))
	if !ok || serviceName.AsString() != "svc" {
		t.Fatalf("service.name = %q (ok=%v), want svc", serviceName.AsString(), ok)
	}
	environment, ok := record.Resource().Set().Value(attribute.Key("deployment.environment"))
	if !ok || environment.AsString() != "test" {
		t.Fatalf("deployment.environment = %q (ok=%v), want test", environment.AsString(), ok)
	}
}
