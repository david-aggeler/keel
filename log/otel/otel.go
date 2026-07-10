// Package otel provides keel/log's optional OpenTelemetry log exporter bridge.
//
// Importing this package opts the consumer into the OpenTelemetry SDK
// dependency. The core github.com/david-aggeler/keel/log package does not import
// this package and remains dependency-light for consumers that do not need OTLP.
package otel

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

const instrumentationName = "github.com/david-aggeler/keel/log/otel"

// Config controls the optional OTLP log exporter handler.
type Config struct {
	// Endpoint is the OTLP HTTP base endpoint. Empty uses the OTel environment
	// defaults.
	Endpoint string
	// Insecure disables TLS for the OTLP HTTP exporter.
	Insecure bool
	// Headers are attached to OTLP HTTP export requests.
	Headers map[string]string

	// ServiceName is stamped as resource attribute service.name when non-empty.
	ServiceName string
	// Resource is merged with the service.name resource. Caller attributes win
	// on duplicate keys.
	Resource *resource.Resource
	// ResourceAttributes are merged into the exported resource.
	ResourceAttributes []attribute.KeyValue

	// Exporter overrides the OTLP HTTP exporter, primarily for hermetic tests.
	Exporter sdklog.Exporter
}

// NewHandler returns an slog handler backed by the OpenTelemetry slog bridge,
// plus a shutdown function that flushes and closes the SDK provider.
//
// DHF-REQ: keel/requirement-22
func NewHandler(ctx context.Context, cfg Config) (slog.Handler, func(context.Context) error, error) {
	exporter := cfg.Exporter
	if exporter == nil {
		opts := make([]otlploghttp.Option, 0, 3)
		if cfg.Endpoint != "" {
			opts = append(opts, otlploghttp.WithEndpointURL(cfg.Endpoint))
		}
		if cfg.Insecure {
			opts = append(opts, otlploghttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlploghttp.WithHeaders(cfg.Headers))
		}
		var err error
		exporter, err = otlploghttp.New(ctx, opts...)
		if err != nil {
			return nil, nil, fmt.Errorf("keel/log/otel: create OTLP log exporter: %w", err)
		}
	}

	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(res),
	)
	handler := otelslog.NewHandler(
		instrumentationName,
		otelslog.WithLoggerProvider(provider),
		otelslog.WithVersion(otel.Version()),
	)
	return handler, provider.Shutdown, nil
}

func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	var resources []*resource.Resource
	if cfg.ServiceName != "" {
		resources = append(resources, resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
		))
	}
	if len(cfg.ResourceAttributes) > 0 {
		resources = append(resources, resource.NewSchemaless(cfg.ResourceAttributes...))
	}
	if cfg.Resource != nil {
		resources = append(resources, cfg.Resource)
	}
	if len(resources) == 0 {
		return resource.DefaultWithContext(ctx), nil
	}
	res := resources[0]
	for _, next := range resources[1:] {
		var err error
		res, err = resource.Merge(res, next)
		if err != nil {
			return nil, fmt.Errorf("keel/log/otel: merge resource: %w", err)
		}
	}
	return res, nil
}
