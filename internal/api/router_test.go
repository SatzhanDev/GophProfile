package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/SatzhanDev/GophProfile/internal/config"
	"github.com/SatzhanDev/GophProfile/internal/domain"
	"github.com/SatzhanDev/GophProfile/internal/handlers"
	"github.com/SatzhanDev/GophProfile/internal/repository"
	"github.com/SatzhanDev/GophProfile/internal/services"
	"github.com/SatzhanDev/GophProfile/web"
)

// Минимальные фейки — этому тесту не нужна настоящая бизнес-логика,
// только чтобы AvatarService и WebHandler вообще собрались и роутер можно
// было построить целиком. Сама бизнес-логика уже проверена в
// internal/services и internal/handlers — здесь цель другая: убедиться,
// что маршруты в NewRouter действительно ведут туда, куда задумано.

type noopRepo struct{}

func (noopRepo) Create(context.Context, *domain.Avatar) error { return nil }
func (noopRepo) GetByID(context.Context, uuid.UUID) (*domain.Avatar, error) {
	return nil, repository.ErrAvatarNotFound
}
func (noopRepo) ListByUserID(context.Context, string) ([]domain.Avatar, error) { return nil, nil }
func (noopRepo) GetLatestByUserID(context.Context, string) (*domain.Avatar, error) {
	return nil, repository.ErrAvatarNotFound
}
func (noopRepo) SoftDelete(context.Context, uuid.UUID) error { return nil }
func (noopRepo) UpdateProcessingStatus(context.Context, uuid.UUID, domain.ProcessingStatus) error {
	return nil
}
func (noopRepo) UpdateThumbnails(context.Context, uuid.UUID, map[string]string) error { return nil }

type noopStorage struct{}

func (noopStorage) Upload(context.Context, string, io.Reader, int64, string) error { return nil }
func (noopStorage) Download(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(nil), nil
}
func (noopStorage) Delete(context.Context, string) error { return nil }

type noopPublisher struct{}

func (noopPublisher) Publish(context.Context, string, any) error { return nil }

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()

	svc := services.NewAvatarService(noopRepo{}, noopStorage{}, noopPublisher{})
	avatarHandler := handlers.NewAvatarHandler(svc)

	webHandler, err := handlers.NewWebHandler(svc, web.TemplatesFS, web.StaticFS)
	require.NoError(t, err)

	return NewRouter(Deps{
		Config: &config.Config{
			CORS:      config.CORSConfig{AllowedOrigins: []string{"*"}},
			RateLimit: config.RateLimitConfig{RequestsPerSecond: 1000, Burst: 1000},
		},
		HealthChecks:  map[string]handlers.Pinger{},
		AvatarHandler: avatarHandler,
		WebHandler:    webHandler,
	})
}

func TestNewRouter_RootRedirectsToUploadForm(t *testing.T) {
	r := newTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	require.Equal(t, "/web/upload", rec.Header().Get("Location"))
}

func TestNewRouter_HealthEndpoint(t *testing.T) {
	r := newTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}

func TestNewRouter_APIAvatarRouteIsWired(t *testing.T) {
	r := newTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/"+uuid.NewString(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// 404 от нашего JSON-хендлера ("Avatar not found"), а не от chi
	// ("404 page not found") — это и подтверждает, что маршрут действительно
	// дошёл до AvatarHandler.GetAvatar, а не до дефолтного NotFoundHandler.
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "Avatar not found")
}

func TestNewRouter_WebUploadFormIsWired(t *testing.T) {
	r := newTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/web/upload", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/html")
}

func TestNewRouter_WebStaticAssetsAreServed(t *testing.T) {
	r := newTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/web/static/style.css", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "css")
}

func TestNewRouter_CORSPreflight(t *testing.T) {
	r := newTestRouter(t)

	// Preflight-запрос, который браузер шлёт сам, перед настоящим
	// cross-origin запросом (например, DELETE с заголовком X-User-ID
	// с фронтенда на другом домене).
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/avatars/"+uuid.NewString(), nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodDelete)
	req.Header.Set("Access-Control-Request-Headers", "X-User-ID")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	require.Contains(t, strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers")), "x-user-id")
}
