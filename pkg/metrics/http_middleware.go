package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// HTTPMiddleware — chi-совместимый middleware, который на каждый запрос
// пишет RED-метрики: HTTPRequestsTotal, HTTPRequestDuration,
// HTTPRequestsInFlight.
//
// Метку route снимаем ПОСЛЕ вызова next.ServeHTTP, а не из r.URL.Path —
// chi заполняет RouteContext шаблоном маршрута (например,
// "/api/v1/avatars/{avatarID}") по ходу сопоставления запроса, и к моменту,
// когда управление возвращается сюда, шаблон уже известен. Если использовать
// вместо него фактический путь запроса, у каждой аватарки (свой UUID
// в пути) была бы своя метка, и метрика "взорвалась" бы по количеству
// уникальных значений (cardinality explosion) — Prometheus и Grafana
// с таким справляются очень плохо.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		HTTPRequestsInFlight.Inc()
		defer HTTPRequestsInFlight.Dec()

		start := time.Now()
		// WrapResponseWriter — обёртка из chi/middleware, которая запоминает
		// код ответа. Обычный http.ResponseWriter такого не даёт: метод
		// WriteHeader ничего не возвращает, а если хендлер вообще не позвал
		// его явно (например, отдал 200 через голый Write), узнать код
		// после факта было бы нечем.
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		status := strconv.Itoa(ww.Status())

		HTTPRequestsTotal.WithLabelValues(r.Method, route, status).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, route, status).Observe(time.Since(start).Seconds())
	})
}
