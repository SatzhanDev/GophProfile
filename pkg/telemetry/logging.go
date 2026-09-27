package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// TraceHandler оборачивает любой другой slog.Handler и на каждую запись
// лога добавляет trace_id/span_id из контекста — если в нём есть активный
// спан. Это и есть "корреляция логов с трейсами" из ТЗ: открыв трейс
// в Jaeger по его trace_id, можно найти в логах (JSON) все строки,
// оставленные во время обработки именно этого запроса — и наоборот,
// увидев в логах ошибку с trace_id, сразу перейти к трейсу с деталями
// (какой запрос, какие SQL-запросы, какие вызовы S3 внутри него были).
//
// Работает только с вызовами вида slog.ErrorContext(ctx, ...)/InfoContext(ctx, ...)
// и т.п. — slog передаёт ctx в Handle только в *Context-вариантах методов;
// обычный slog.Info(...) без контекста добавить в лог нечего.
type TraceHandler struct {
	slog.Handler
}

// NewTraceHandler оборачивает next, добавляя корреляцию с трейсами.
func NewTraceHandler(next slog.Handler) *TraceHandler {
	return &TraceHandler{Handler: next}
}

// Handle добавляет trace_id/span_id (если в ctx есть активный спан)
// и передаёт запись дальше, во вложенный handler.
func (h *TraceHandler) Handle(ctx context.Context, record slog.Record) error {
	sc := trace.SpanContextFromContext(ctx)
	if sc.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, record)
}

// WithAttrs переопределён явно, а не унаследован через встраивание: у
// slog.Handler он возвращает новый Handler, и без явного переопределения
// это был бы "голый" внутренний handler (например, *slog.JSONHandler) —
// обёртка TraceHandler потерялась бы, и любой логгер, созданный через
// logger.With(...), перестал бы добавлять trace_id/span_id.
func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup по той же причине, что и WithAttrs, тоже переопределён явно,
// чтобы не терять обёртку TraceHandler.
func (h *TraceHandler) WithGroup(name string) slog.Handler {
	return &TraceHandler{Handler: h.Handler.WithGroup(name)}
}
