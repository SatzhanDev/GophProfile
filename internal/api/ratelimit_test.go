package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIPRateLimiter_AllowsBurstThenRejects(t *testing.T) {
	// burst = 2: первые два запроса проходят, третий (тот же IP, та же
	// секунда) должен получить 429 — токены ещё не успели пополниться.
	limiter := newIPRateLimiter(1, 2)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	newRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/x", nil)
		req.RemoteAddr = "203.0.113.1:12345"
		return req
	}

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, newRequest())
		require.Equal(t, http.StatusOK, rec.Code, "request %d within burst should pass", i+1)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, newRequest())
	require.Equal(t, http.StatusTooManyRequests, rec.Code, "request beyond burst should be rejected")
}

func TestIPRateLimiter_TracksClientsSeparately(t *testing.T) {
	limiter := newIPRateLimiter(1, 1)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, addr := range []string{"203.0.113.1:1", "203.0.113.2:1"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/x", nil)
		req.RemoteAddr = addr
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "each new IP gets its own budget: %s", addr)
	}
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:54321"
	require.Equal(t, "203.0.113.5", clientIP(req))

	req.RemoteAddr = "not-a-valid-host-port"
	require.Equal(t, "not-a-valid-host-port", clientIP(req))
}
