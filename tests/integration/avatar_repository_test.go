//go:build integration

// Интеграционные тесты, в отличие от юнит-тестов рядом с кодом (*_test.go
// в internal/...), поднимают настоящий PostgreSQL в Docker-контейнере через
// testcontainers-go и проверяют репозиторий целиком, вместе с реальным SQL.
// Помечены build tag "integration" и НЕ запускаются обычным `go test ./...` —
// иначе каждый `go test` требовал бы Docker и был бы намного медленнее.
// Запуск: go test -tags=integration ./tests/... -v
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/SatzhanDev/GophProfile/internal/domain"
	"github.com/SatzhanDev/GophProfile/internal/repository"
	"github.com/SatzhanDev/GophProfile/pkg/postgres"
)

// AvatarRepositorySuite — testify/suite: SetupSuite поднимает контейнер один
// раз на весь набор тестов (дорого), а TearDownTest чистит таблицу после
// каждого теста (дёшево), чтобы тесты не зависели друг от друга по данным.
type AvatarRepositorySuite struct {
	suite.Suite

	container *tcpostgres.PostgresContainer
	pool      *pgxpool.Pool
	repo      *repository.PostgresAvatarRepository
}

func TestAvatarRepositorySuite(t *testing.T) {
	suite.Run(t, new(AvatarRepositorySuite))
}

func (s *AvatarRepositorySuite) SetupSuite() {
	ctx := context.Background()

	// Примечание: если после `go mod tidy` у тебя подтянется более новая
	// версия testcontainers-go, где postgres.RunContainer помечен
	// deprecated — замени на postgres.Run с тем же набором опций,
	// в какой-то версии функцию просто переименовали.
	container, err := tcpostgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:16-alpine"),
		tcpostgres.WithDatabase("gophprofile_test"),
		tcpostgres.WithUsername("gophprofile"),
		tcpostgres.WithPassword("gophprofile"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	s.Require().NoError(err)
	s.container = container

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	s.Require().NoError(err)

	s.Require().NoError(postgres.RunMigrations("../../migrations", dsn))

	pool, err := postgres.NewPool(ctx, dsn)
	s.Require().NoError(err)
	s.pool = pool

	s.repo = repository.NewPostgresAvatarRepository(pool)
}

func (s *AvatarRepositorySuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.container != nil {
		s.Require().NoError(s.container.Terminate(context.Background()))
	}
}

func (s *AvatarRepositorySuite) TearDownTest() {
	_, err := s.pool.Exec(context.Background(), "TRUNCATE avatars")
	s.Require().NoError(err)
}

func (s *AvatarRepositorySuite) TestCreateAndGetByID() {
	ctx := context.Background()
	avatar := &domain.Avatar{
		UserID:           "user-1",
		FileName:         "photo.jpg",
		MimeType:         "image/jpeg",
		SizeBytes:        1024,
		S3Key:            "originals/test.jpg",
		UploadStatus:     domain.UploadStatusCompleted,
		ProcessingStatus: domain.ProcessingStatusPending,
	}

	s.Require().NoError(s.repo.Create(ctx, avatar))
	s.NotEqual(uuid.Nil, avatar.ID)

	got, err := s.repo.GetByID(ctx, avatar.ID)
	s.Require().NoError(err)
	s.Equal(avatar.UserID, got.UserID)
	s.Equal(avatar.S3Key, got.S3Key)
}

func (s *AvatarRepositorySuite) TestGetByID_NotFound() {
	_, err := s.repo.GetByID(context.Background(), uuid.New())
	s.ErrorIs(err, repository.ErrAvatarNotFound)
}

func (s *AvatarRepositorySuite) TestSoftDelete_HidesFromGetByID() {
	ctx := context.Background()
	avatar := &domain.Avatar{
		UserID: "user-1", FileName: "a.jpg", MimeType: "image/jpeg", SizeBytes: 1, S3Key: "originals/a.jpg",
	}
	s.Require().NoError(s.repo.Create(ctx, avatar))

	s.Require().NoError(s.repo.SoftDelete(ctx, avatar.ID))

	_, err := s.repo.GetByID(ctx, avatar.ID)
	s.ErrorIs(err, repository.ErrAvatarNotFound)
}

func (s *AvatarRepositorySuite) TestGetLatestByUserID_ReturnsMostRecent() {
	ctx := context.Background()

	first := &domain.Avatar{UserID: "user-2", FileName: "a.jpg", MimeType: "image/jpeg", SizeBytes: 1, S3Key: "originals/a.jpg"}
	s.Require().NoError(s.repo.Create(ctx, first))

	time.Sleep(10 * time.Millisecond) // гарантируем разный created_at у второй записи

	second := &domain.Avatar{UserID: "user-2", FileName: "b.jpg", MimeType: "image/jpeg", SizeBytes: 1, S3Key: "originals/b.jpg"}
	s.Require().NoError(s.repo.Create(ctx, second))

	latest, err := s.repo.GetLatestByUserID(ctx, "user-2")
	s.Require().NoError(err)
	s.Equal(second.ID, latest.ID)
}

func (s *AvatarRepositorySuite) TestUpdateThumbnailsAndStatus() {
	ctx := context.Background()
	avatar := &domain.Avatar{UserID: "user-3", FileName: "a.jpg", MimeType: "image/jpeg", SizeBytes: 1, S3Key: "originals/a.jpg"}
	s.Require().NoError(s.repo.Create(ctx, avatar))

	thumbs := map[string]string{"100x100": "thumbnails/a/100x100.jpg"}
	s.Require().NoError(s.repo.UpdateThumbnails(ctx, avatar.ID, thumbs))
	s.Require().NoError(s.repo.UpdateProcessingStatus(ctx, avatar.ID, domain.ProcessingStatusCompleted))

	got, err := s.repo.GetByID(ctx, avatar.ID)
	s.Require().NoError(err)
	s.Equal(thumbs, got.ThumbnailS3Keys)
	s.Equal(domain.ProcessingStatusCompleted, got.ProcessingStatus)
}
