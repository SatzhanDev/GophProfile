package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakePinger struct {
	err error
}

func (p fakePinger) Ping(_ context.Context) error {
	return p.err
}

func TestHealthHandler_AllComponentsOK(t *testing.T) {
	h := NewHealthHandler(map[string]Pinger{"postgres": fakePinger{}})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Status     string                    `json:"status"`
		Components map[string]map[string]any `json:"components"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "ok", body.Status)
	require.Equal(t, "ok", body.Components["postgres"]["status"])
}

func TestHealthHandler_ComponentDown(t *testing.T) {
	h := NewHealthHandler(map[string]Pinger{
		"postgres": fakePinger{},
		"s3":       fakePinger{err: errors.New("connection refused")},
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var body struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "degraded", body.Status)
}
