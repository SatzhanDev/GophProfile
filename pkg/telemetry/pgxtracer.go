package telemetry

import (
	"context"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// pgxSpanKey — ключ контекста, под которым PgxTracer прячет span между
// вызовами TraceQueryStart и TraceQueryEnd. pgx вызывает их по отдельности
// (не одним callback'ом, оборачивающим запрос целиком), поэтому span нужно
// как-то донести из первого вызова до второго — контекст для этого и создан.
type pgxSpanKey struct{}

// PgxTracer реализует интерфейс pgx.QueryTracer. pgx сам вызывает его
// методы вокруг КАЖДОГО запроса к БД — не нужно вручную оборачивать каждый
// метод репозитория в span, это подключается один раз на уровне пула
// соединений (см. pkg/postgres.NewPool).
type PgxTracer struct {
	tracer trace.Tracer
}

// NewPgxTracer создаёт трейсер для запросов к Postgres.
func NewPgxTracer() *PgxTracer {
	return &PgxTracer{tracer: otel.Tracer("postgres")}
}

// TraceQueryStart вызывается pgx-ом прямо перед выполнением запроса.
func (t *PgxTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx, span := t.tracer.Start(ctx, "db.query",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.statement", data.SQL),
		),
	)
	return context.WithValue(ctx, pgxSpanKey{}, span)
}

// TraceQueryEnd вызывается pgx-ом сразу после завершения запроса —
// успешного или нет.
func (t *PgxTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span, ok := ctx.Value(pgxSpanKey{}).(trace.Span)
	if !ok {
		return
	}
	defer span.End()

	if data.Err != nil {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	}
}
