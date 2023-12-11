package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc"
	"log"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
)

var tracer = otel.GetTracerProvider().Tracer("main")

var commonLabels []attribute.KeyValue
var reveiveCount metric.Int64Counter
var requestLatency metric.Float64Histogram
var requestCount metric.Int64Counter

type Entry struct {
	Message   string `json:"message"`
	Severity  string `json:"severity,omitempty"`
	Trace     string `json:"logging.googleapis.com/trace,omitempty"`
	SpanId    string `json:"logging.googleapis.com/spanId,omitempty"`
	Component string `json:"component,omitempty"`
}

// String renders an entry structure to the JSON format expected by Cloud Logging.
func (e Entry) String() string {
	if e.Severity == "" {
		e.Severity = "INFO"
	}
	out, err := json.Marshal(e)
	if err != nil {
		log.Printf("json.Marshal: %v", err)
	}
	return string(out)
}

func loadEnvFile() {
	err := godotenv.Load()
	if err != nil {
		log.Printf("Not found .env file: %v", err)
	}
}

func makeTraceIdFmt(traceId string) string {
	return fmt.Sprintf("projects/%s/traces/%s", os.Getenv("PROJECT_ID"), traceId)
}

func initTraceProvider(ctx context.Context, otelAgentAddr string, serviceName string) func() {

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(
			// the service name used to display traces in backends
			semconv.ServiceNameKey.String(serviceName),
		),
	)
	if err != nil {
		return nil
	}

	metricExp, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithEndpoint(otelAgentAddr))
	if err != nil {
		return nil
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(
				metricExp,
				sdkmetric.WithInterval(2*time.Second),
			),
		),
	)
	otel.SetMeterProvider(meterProvider)

	traceClient := otlptracegrpc.NewClient(
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(otelAgentAddr),
		otlptracegrpc.WithDialOption(grpc.WithBlock()))
	exporter, err := otlptrace.New(ctx, traceClient)
	if err != nil {
		return nil
	}

	bsp := sdktrace.NewBatchSpanProcessor(exporter)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	otel.SetTracerProvider(tracerProvider)

	return func() {
		cxt, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := exporter.Shutdown(cxt); err != nil {
			otel.Handle(err)
		}
		// pushes any last exports to the receiver
		if err := meterProvider.Shutdown(cxt); err != nil {
			otel.Handle(err)
		}
	}
}

type handler struct {
	cli http.Client
}

func newHandler() *handler {
	hc := http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	return &handler{
		cli: hc,
	}
}

func (h *handler) sleep(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, span := tracer.Start(ctx, "sleep")
	defer span.End()

	l := []attribute.KeyValue{
		attribute.String("trace_id", span.SpanContext().TraceID().String()),
		attribute.String("span_id", span.SpanContext().SpanID().String()),
	}
	labels := append(commonLabels, l...)
	reveiveCount.Add(ctx, 1, metric.WithAttributes(labels...))

	log.Println(Entry{
		Severity:  "INFO",
		Message:   "Handling request",
		Component: os.Getenv("APP_NAME"),
		Trace:     makeTraceIdFmt(span.SpanContext().TraceID().String()),
		SpanId:    span.SpanContext().SpanID().String(),
	})

	time.Sleep(2 * time.Second)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`{"message": "Good Morning. 'I'm wake up.", "version": %s}`, os.Getenv("APP_VERSION"))))
}

func (h *handler) sleepAndCall(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	child, span := tracer.Start(ctx, "sleepAndCall")
	defer span.End()

	l := []attribute.KeyValue{
		attribute.String("trace_id", span.SpanContext().TraceID().String()),
		attribute.String("span_id", span.SpanContext().SpanID().String()),
	}
	labels := append(commonLabels, l...)
	reveiveCount.Add(child, 1, metric.WithAttributes(labels...))

	log.Println(Entry{
		Severity:  "INFO",
		Message:   "Handle request. This function will call other service",
		Component: os.Getenv("APP_NAME"),
		Trace:     makeTraceIdFmt(span.SpanContext().TraceID().String()),
		SpanId:    span.SpanContext().SpanID().String(),
	})

	time.Sleep(2 * time.Second)

	hreq, err := http.NewRequestWithContext(child, "GET", os.Getenv("ENDPOINT"), nil)
	if err != nil {
		log.Println(Entry{
			Severity:  "ERROR",
			Message:   fmt.Sprintf("Failed create http request: %v", err),
			Component: os.Getenv("APP_NAME"),
			Trace:     makeTraceIdFmt(span.SpanContext().TraceID().String()),
			SpanId:    span.SpanContext().SpanID().String(),
		})
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	startTime := time.Now()

	resp, err := h.cli.Do(hreq)
	if err != nil {
		log.Println(Entry{
			Severity:  "ERROR",
			Message:   fmt.Sprintf("Failed call request: %v", err),
			Component: os.Getenv("APP_NAME"),
			Trace:     makeTraceIdFmt(span.SpanContext().TraceID().String()),
			SpanId:    span.SpanContext().SpanID().String(),
		})
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	latencyMs := float64(time.Since(startTime))
	resp.Body.Close()

	requestLatency.Record(child, latencyMs, metric.WithAttributes(labels...))
	requestCount.Add(child, 1, metric.WithAttributes(labels...))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message": "Good Morning. 'I'm wake up and call other service."}`))

}

func main() {
	loadEnvFile()

	ctx := context.Background()
	port := os.Getenv("APP_PORT")
	appName := os.Getenv("APP_NAME")
	oltpAddr := os.Getenv("OTEL_AGENT_ENDPOINT")

	log.SetFlags(0)

	shutdown := initTraceProvider(ctx, oltpAddr, appName)
	defer shutdown()

	meter := otel.Meter("server-meter")
	commonLabels = []attribute.KeyValue{
		attribute.String("project_id", os.Getenv("PROJECT_ID")),
	}

	reveiveCount, _ = meter.Int64Counter(
		"api_server/receive_counts",
		metric.WithDescription("The number of receive processed"),
	)

	requestLatency, _ = meter.Float64Histogram(
		"api_server/request_latency",
		metric.WithDescription("The latency of requests processed"),
	)

	requestCount, _ = meter.Int64Counter(
		"api_server/request_counts",
		metric.WithDescription("The number of requests processed"),
	)

	h := newHandler()
	mux := http.NewServeMux()
	mux.Handle("/api/v1/sleep", http.HandlerFunc(h.sleep))
	mux.Handle("/api/v1/chain", http.HandlerFunc(h.sleepAndCall))
	//mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status": "OK"}`))
	}))

	log.Println(Entry{
		Severity:  "INFO",
		Message:   "Starting Http Server...",
		Component: appName,
	})
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), otelhttp.NewHandler(mux, "server",
		otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
	)))
}
