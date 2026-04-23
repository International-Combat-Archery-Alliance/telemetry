package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Options configure the OpenTelemetry initialization.
type Options struct {
	// ServiceName is the name of the service exporting telemetry.
	// Required.
	ServiceName string

	// Endpoint is the OTLP collector endpoint (host:port).
	// Optional; defaults to localhost:4317 if empty.
	Endpoint string

	// APIKey is an optional API key sent as the "api-key" header.
	// When empty, the connection will use TLS unless Insecure is true.
	APIKey string

	// Insecure forces an insecure gRPC connection.
	Insecure bool

	// BatchTimeout is the maximum wait time before a batch is exported.
	// Zero defaults to 5s (1s if running in Lambda).
	BatchTimeout time.Duration

	// ExportTimeout is the maximum duration for a single export.
	// Zero defaults to 30s (5s if running in Lambda).
	ExportTimeout time.Duration

	// ErrorHandler is called when the OpenTelemetry SDK encounters an error.
	// Optional.
	ErrorHandler func(error)

	// Lambda provides AWS Lambda runtime attributes.
	// Optional; use nil when not running in Lambda.
	Lambda *LambdaInfo
}

// LambdaInfo holds AWS Lambda runtime attributes.
type LambdaInfo struct {
	FunctionName string
	Region       string
	Version      string
}

// LambdaInfoFromEnv returns LambdaInfo populated from standard AWS Lambda
// environment variables. It returns nil if AWS_LAMBDA_FUNCTION_NAME is not set.
func LambdaInfoFromEnv() *LambdaInfo {
	fn := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")
	if fn == "" {
		return nil
	}
	return &LambdaInfo{
		FunctionName: fn,
		Region:       os.Getenv("AWS_REGION"),
		Version:      os.Getenv("AWS_LAMBDA_FUNCTION_VERSION"),
	}
}

// Init sets up OpenTelemetry tracing and returns shutdown and flush callbacks.
// Both callbacks are safe to call even when nil error is returned (they become no-ops).
func Init(ctx context.Context, opts Options) (shutdown func(context.Context) error, flush func(context.Context) error, err error) {
	if opts.ServiceName == "" {
		return nil, nil, fmt.Errorf("telemetry.Init: ServiceName is required")
	}

	endpoint := opts.Endpoint
	if endpoint == "" {
		endpoint = "localhost:4317"
	}

	grpcOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}

	if opts.APIKey != "" {
		grpcOpts = append(grpcOpts, otlptracegrpc.WithHeaders(map[string]string{
			"api-key": opts.APIKey,
		}))
	}
	if opts.Insecure || opts.APIKey == "" {
		grpcOpts = append(grpcOpts, otlptracegrpc.WithInsecure())
	}

	traceExporter, err := otlptracegrpc.New(ctx, grpcOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("telemetry.Init: failed to create trace exporter: %w", err)
	}

	if opts.ErrorHandler != nil {
		otel.SetErrorHandler(otel.ErrorHandlerFunc(opts.ErrorHandler))
	}

	attrs := []attribute.KeyValue{
		semconv.ServiceName(opts.ServiceName),
	}
	attrs = append(attrs, lambdaAttributes(opts.Lambda)...)

	res, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		return nil, nil, fmt.Errorf("telemetry.Init: failed to create resource: %w", err)
	}

	batchTimeout := opts.BatchTimeout
	exportTimeout := opts.ExportTimeout

	if opts.Lambda != nil {
		if batchTimeout == 0 {
			batchTimeout = 1 * time.Second
		}
		if exportTimeout == 0 {
			exportTimeout = 5 * time.Second
		}
	} else {
		if batchTimeout == 0 {
			batchTimeout = 5 * time.Second
		}
		if exportTimeout == 0 {
			exportTimeout = 30 * time.Second
		}
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter,
			sdktrace.WithBatchTimeout(batchTimeout),
			sdktrace.WithExportTimeout(exportTimeout),
		),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return func(ctx context.Context) error {
			return tp.Shutdown(ctx)
		}, func(ctx context.Context) error {
			return tp.ForceFlush(ctx)
		}, nil
}

// InstrumentedHTTPClient returns an *http.Client whose transport is wrapped
// with OpenTelemetry instrumentation.
func InstrumentedHTTPClient() *http.Client {
	return &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
}

// InstrumentAWSConfig adds OpenTelemetry instrumentation to all AWS SDK v2
// API calls made with the given config.
func InstrumentAWSConfig(cfg *aws.Config) {
	otelaws.AppendMiddlewares(&cfg.APIOptions)
}

// RunWithSpan executes fn inside a new span. If fn returns an error, the error
// is recorded on the span before being returned.
func RunWithSpan(ctx context.Context, tracer trace.Tracer, name string, fn func(context.Context) error) error {
	ctx, span := tracer.Start(ctx, name)
	defer span.End()
	if err := fn(ctx); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}

func lambdaAttributes(info *LambdaInfo) []attribute.KeyValue {
	if info == nil {
		return nil
	}
	var attrs []attribute.KeyValue
	if info.FunctionName != "" {
		attrs = append(attrs, semconv.FaaSName(info.FunctionName))
	}
	if info.Region != "" {
		attrs = append(attrs, semconv.CloudRegion(info.Region))
	}
	if info.Version != "" {
		attrs = append(attrs, semconv.FaaSVersion(info.Version))
	}
	return attrs
}
