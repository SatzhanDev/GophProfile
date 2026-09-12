// Package telemetry настраивает OpenTelemetry: откуда брать трейсы и как их
// отправлять в Jaeger, плюс склейку логов с трейсами. Остальной код
// приложения не знает, что спаны вообще куда-то отправляются — он просто
// вызывает otel.Tracer(...).Start(...), а куда реально уйдут собранные
// данные, решается один раз здесь, при старте процесса.
package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// InitTracer настраивает глобальный TracerProvider приложения:
//   - экспортёр отправляет спаны в Jaeger по протоколу OTLP/gRPC
//     (современный Jaeger сам умеет принимать OTLP, отдельный
//     "jaeger exporter" из старых версий otel-go для этого не нужен);
//   - resource — метаданные источника трейса (имя сервиса), по нему
//     в Jaeger UI трейсы от server и worker будет видно раздельно;
//   - propagator — формат, которым контекст трейсинга передаётся между
//     процессами: W3C Trace Context (стандартный заголовок "traceparent")
//     и Baggage. Без этого HTTP-запрос и обработка в воркере выглядели бы
//     как два несвязанных трейса, а не одна цепочка.
//
// Возвращает shutdown-функцию — её нужно вызвать при остановке приложения,
// чтобы отправить в Jaeger спаны, которые ещё лежат в буфере экспортёра.
func InitTracer(ctx context.Context, serviceName, otlpEndpoint string) (func(context.Context) error, error) {
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(otlpEndpoint),
		otlptracegrpc.WithInsecure(), // локальный Jaeger внутри docker-сети, TLS не нужен
	)
	if err != nil {
		return nil, fmt.Errorf("create otlp exporter: %w", err)
	}

	res := resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceNameKey.String(serviceName))

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter), // спаны отправляются пачками, не по одному — дешевле по сети
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}
