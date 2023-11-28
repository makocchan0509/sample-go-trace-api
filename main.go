package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/joho/godotenv"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"log"
	"net/http"
	"os"
	"time"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
)

// var logger *logging.Logger
var tracer = otel.GetTracerProvider().Tracer("main")

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

func initTraceProvider(ctx context.Context, project string) *sdktrace.TracerProvider {
	exporter, err := texporter.New(texporter.WithProjectID(project))
	if err != nil {
		log.Fatalf("texporter.New: %v", err)
	}

	res, err := resource.New(ctx,
		resource.WithDetectors(gcp.NewDetector()),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(os.Getenv("APP_NAME")),
		),
	)
	if err != nil {
		log.Fatalf("resource.New: %v", err)
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
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
	_, span := tracer.Start(r.Context(), "sleep")
	defer span.End()

	log.Println(Entry{
		Severity:  "INFO",
		Message:   "Handling request",
		Component: os.Getenv("APP_NAME"),
		Trace:     makeTraceIdFmt(span.SpanContext().TraceID().String()),
		SpanId:    span.SpanContext().SpanID().String(),
	})

	time.Sleep(3 * time.Second)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`{"message": "Good Morning. 'I'm wake up.", "version": %s}`, os.Getenv("APP_VERSION"))))
}

func (h *handler) sleepAndCall(w http.ResponseWriter, r *http.Request) {

	ctx, span := tracer.Start(r.Context(), "sleepAndCall")
	defer span.End()

	log.Println(Entry{
		Severity:  "INFO",
		Message:   "Handle request. This function will call other service",
		Component: os.Getenv("APP_NAME"),
		Trace:     makeTraceIdFmt(span.SpanContext().TraceID().String()),
		SpanId:    span.SpanContext().SpanID().String(),
	})

	time.Sleep(3 * time.Second)

	hreq, err := http.NewRequestWithContext(ctx, "GET", os.Getenv("ENDPOINT"), nil)
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
	resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message": "Good Morning. 'I'm wake up and call other service."}`))

}

func main() {
	loadEnvFile()

	ctx := context.Background()
	project := os.Getenv("PROJECT_ID")
	port := os.Getenv("APP_PORT")
	appName := os.Getenv("APP_NAME")

	log.SetFlags(0)

	tp := initTraceProvider(ctx, project)
	defer tp.Shutdown(ctx)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	h := newHandler()
	mux := http.NewServeMux()
	mux.Handle("/api/v1/sleep", http.HandlerFunc(h.sleep))
	mux.Handle("/api/v1/chain", http.HandlerFunc(h.sleepAndCall))
	mux.Handle("/metrics", promhttp.Handler())

	log.Println(Entry{
		Severity:  "INFO",
		Message:   "Starting Http Server...",
		Component: appName,
	})
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", port), otelhttp.NewHandler(mux, "server",
		otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
	)))
}
