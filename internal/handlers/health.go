// Package handlers содержит HTTP-обработчики (уровень transport в архитектуре сервиса).
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Pinger — минимальный интерфейс, который нужен health-чеку от зависимости:
// "проверь, что ты жив". Ему удовлетворяет *pgxpool.Pool, а позже —
// и клиенты S3/брокера. Хендлер не завязан на конкретную библиотеку pgx,
// это упрощает unit-тестирование (можно подставить фейковую реализацию).
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler обрабатывает GET /health и проверяет доступность
// подключённых компонентов. Пока это только PostgreSQL — S3 и брокер
// добавятся в своих инкрементах.
type HealthHandler struct {
	db Pinger
}

// NewHealthHandler создаёт HealthHandler.
func NewHealthHandler(db Pinger) *HealthHandler {
	return &HealthHandler{db: db}
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

	components := map[string]componentStatus{}
	overall := "ok"

	if err := h.db.Ping(ctx); err != nil {
		components["postgres"] = componentStatus{Status: "down", Error: err.Error()}
		overall = "degraded"
	} else {
		components["postgres"] = componentStatus{Status: "ok"}
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
