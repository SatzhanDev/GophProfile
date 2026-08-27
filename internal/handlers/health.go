// Package handlers содержит HTTP-обработчики (уровень transport в архитектуре сервиса).
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Pinger — минимальный интерфейс, который нужен health-чеку от зависимости:
// "проверь, что ты жив". Ему удовлетворяют *pgxpool.Pool и *s3.Client,
// а позже — и клиент брокера. Хендлер не завязан на конкретные библиотеки,
// это упрощает unit-тестирование (можно подставить фейковую реализацию).
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler обрабатывает GET /health и проверяет доступность всех
// переданных ему компонентов. Какие именно компоненты проверять —
// решает вызывающий код (main.go), а не сам хендлер.
type HealthHandler struct {
	checks map[string]Pinger
}

// NewHealthHandler создаёт HealthHandler. checks — карта "имя компонента
// для JSON-ответа" -> "чем его проверять", например:
//
//	map[string]Pinger{"postgres": dbPool, "s3": s3Client}
func NewHealthHandler(checks map[string]Pinger) *HealthHandler {
	return &HealthHandler{checks: checks}
}

type componentStatus struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type healthResponse struct {
	Status     string                     `json:"status"`
	Time       time.Time                  `json:"time"`
	Components map[string]componentStatus `json:"components"`
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	components := make(map[string]componentStatus, len(h.checks))
	overall := "ok"

	for name, checker := range h.checks {
		if err := checker.Ping(ctx); err != nil {
			components[name] = componentStatus{Status: "down", Error: err.Error()}
			overall = "degraded"
			continue
		}
		components[name] = componentStatus{Status: "ok"}
	}

	resp := healthResponse{
		Status:     overall,
		Time:       time.Now().UTC(),
		Components: components,
	}

	httpStatus := http.StatusOK
	if overall != "ok" {
		httpStatus = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(resp)
}
