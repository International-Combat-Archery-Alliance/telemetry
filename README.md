# ICAA Telemetry

A Go library for OpenTelemetry instrumentation used across the International Combat Archery Alliance's services. Provides a unified way to initialize tracing, instrument HTTP clients, and add observability to AWS SDK calls.

## Features

### OpenTelemetry Initialization
- **OTLP gRPC Exporter**: Configurable endpoint, API key headers, and TLS settings
- **Lambda-Aware Defaults**: Automatic batch timeout adjustments for AWS Lambda environments
- **Custom Error Handling**: Optional error callback for SDK export failures
- **Resource Attributes**: Service name and optional AWS Lambda runtime metadata

### HTTP Instrumentation
- **Instrumented HTTP Client**: `*http.Client` with `otelhttp.NewTransport` for automatic outgoing request tracing

### AWS SDK Instrumentation
- **Config Wrapper**: One-line helper to add OTEL middleware to AWS SDK v2 API options

## Installation

```bash
go get github.com/International-Combat-Archery-Alliance/telemetry
```

## Usage

### Basic Initialization

```go
package main

import (
    "context"
    "log/slog"
    "os"
    "time"

    "github.com/International-Combat-Archery-Alliance/telemetry"
)

func main() {
    ctx := context.Background()
    logger := slog.Default()

    shutdown, flush, err := telemetry.Init(ctx, telemetry.Options{
        ServiceName: "my-service",
        Endpoint:    "localhost:4317",
        ErrorHandler: func(err error) {
            logger.Error("otel error", slog.String("error", err.Error()))
        },
    })
    if err != nil {
        logger.Error("failed to init telemetry", slog.String("error", err.Error()))
        os.Exit(1)
    }
    defer func() {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        if err := shutdown(ctx); err != nil {
            logger.Error("failed to shutdown telemetry", slog.String("error", err.Error()))
        }
    }()

    // Run your application...
}
```

### With New Relic

```go
shutdown, flush, err := telemetry.Init(ctx, telemetry.Options{
    ServiceName: "my-service",
    Endpoint:    "otlp.nr-data.net:4317",
    APIKey:      "my-new-relic-api-key",
})
```

### AWS Lambda

```go
shutdown, flush, err := telemetry.Init(ctx, telemetry.Options{
    ServiceName:  "my-lambda-function",
    Endpoint:     os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
    Lambda:       telemetry.LambdaInfoFromEnv(),
    BatchTimeout: 1 * time.Second,
})
```

### AWS SDK Instrumentation

```go
import (
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/International-Combat-Archery-Alliance/telemetry"
)

cfg, err := config.LoadDefaultConfig(ctx)
if err != nil {
    return err
}

telemetry.InstrumentAWSConfig(&cfg)
// All AWS SDK calls made with cfg are now traced
```

### HTTP Client Instrumentation

```go
client := telemetry.InstrumentedHTTPClient()
// Outgoing HTTP requests made with client are now traced
```

## Configuration

### Options

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `ServiceName` | Yes | — | Name of the service exporting telemetry |
| `Endpoint` | No | `localhost:4317` | OTLP collector endpoint (`host:port`) |
| `APIKey` | No | — | API key sent as `api-key` header |
| `Insecure` | No | `false` | Force insecure gRPC connection |
| `BatchTimeout` | No | `5s` (or `1s` in Lambda) | Max wait before exporting a batch |
| `ExportTimeout` | No | `30s` (or `5s` in Lambda) | Max duration for a single export |
| `ErrorHandler` | No | — | Callback for OTEL SDK errors |
| `Lambda` | No | — | AWS Lambda runtime attributes |

## License

Licensed under the GNU Affero General Public License v3.0. See [LICENSE](LICENSE) for details.

## Contributing

This library is part of the International Combat Archery Alliance's software infrastructure. Contributions should follow the project's coding standards and include appropriate tests.
