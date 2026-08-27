package handlers

import (
	"encoding/json"
	"net/http"
)

// writeJSON сериализует payload в JSON и пишет его в ответ с нужным статусом.
// Общий хелпер, чтобы не повторять один и тот же код кодирования в каждом хендлере.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeError — частный случай writeJSON для ответов вида {"error": "..."}.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
