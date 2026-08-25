// Package handlers содержит HTTP-обработчики (уровень transport в архитектуре сервиса).
package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

// HealthHandler обрабатывает GET /health.
// Пока просто сообщает, что процесс жив. Когда появятся PostgreSQL,
// S3 и брокер — сюда добавим реальные проверки их доступности.
type HealthHandler struct{}

// NewHealthHandler создаёт HealthHandler.
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

type healthResponse struct {
	Status string    `json:"status"`
	Time   time.Time `json:"time"`
}

func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Status: "ok",
		Time:   time.Now().UTC(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
