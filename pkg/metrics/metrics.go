// Package metrics содержит все Prometheus-метрики сервиса: технические
// RED-метрики для HTTP (Rate/Errors/Duration), бизнес-метрики (сколько
// аватарок загружено/удалено, сколько байт они занимают в S3) и
// инфраструктурные метрики (пул соединений с БД, обработка событий воркером).
//
// Метрики регистрируются один раз при инициализации пакета через promauto
// (обёртка над prometheus.NewCounterVec/NewHistogram и т.п., которая сразу
// же регистрирует метрику в глобальном prometheus.DefaultRegisterer) и
// хранятся в пакетных переменных. Это стандартный для client_golang способ:
// сама метрика — глобальный счётчик процесса, а не поле какой-то структуры,
// поэтому её можно увеличивать из любого места (HTTP-хендлер, сервис,
// воркер) без прокидывания зависимостей через конструкторы.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// --- HTTP RED-метрики (Rate, Errors, Duration) ---

var (
	// HTTPRequestsTotal — количество запросов (Rate). "Errors" из RED
	// получают из неё же PromQL-запросом с фильтром по status=~"5..".
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gophprofile_http_requests_total",
		Help: "Total number of HTTP requests to the API, by method, route and status.",
	}, []string{"method", "route", "status"})

	// HTTPRequestDuration — задержка ответа (Duration). Бакеты по
	// умолчанию (5ms .. 10s) хорошо подходят для обычного HTTP API;
	// для эндпоинта загрузки файлов это будет видно как более длинный хвост.
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "gophprofile_http_request_duration_seconds",
		Help:    "HTTP request latency in seconds, by method, route and status.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route", "status"})

	// HTTPRequestsInFlight — сколько запросов обрабатывается прямо сейчас.
	// Формально не часть классической RED-триады, но обычно идёт рядом
	// с ней и хорошо показывает насыщение сервиса под нагрузкой.
	HTTPRequestsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gophprofile_http_requests_in_flight",
		Help: "Number of HTTP API requests currently being processed.",
	})
)

// --- Бизнес-метрики ---

var (
	// AvatarUploadsTotal — сколько загрузок обработал сервис, с разбивкой
	// по итогу: "success", "validation_error" (не тот формат/слишком большой
	// файл), "storage_error" (не удалось записать в S3 или в БД).
	AvatarUploadsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gophprofile_avatar_uploads_total",
		Help: "Total number of avatar upload attempts, by outcome.",
	}, []string{"status"})

	// AvatarUploadDuration — сколько времени занимает AvatarService.Upload
	// целиком: валидация + запись оригинала в S3 + запись метаданных в БД.
	// Генерация миниатюр сюда не входит — она асинхронная, происходит уже
	// в воркере и видна в WorkerJobDuration.
	AvatarUploadDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "gophprofile_avatar_upload_duration_seconds",
		Help:    "Duration of the avatar upload use case in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	// AvatarDeletesTotal — сколько мягких удалений обработал сервис.
	AvatarDeletesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gophprofile_avatar_deletes_total",
		Help: "Total number of avatar delete attempts, by outcome.",
	}, []string{"status"})

	// AvatarStorageBytes — приблизительный суммарный объём оригиналов
	// аватарок в S3: растёт на размер файла при успешной загрузке,
	// уменьшается на размер оригинала при мягком удалении.
	//
	// Оценка приблизительная и сознательно не учитывает миниатюры: их
	// размер становится известен только в воркере, уже после того, как
	// HTTP-запрос на загрузку завершился, а метаданные о миниатюрах в БД
	// хранят только ключи S3, не размеры. Для тренда на дашборде ("хранилище
	// растёт / не растёт") этого достаточно; для точного биллинга по байтам
	// нужен был бы отдельный подсчёт, это уже за рамками текущего инкремента.
	AvatarStorageBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gophprofile_avatar_storage_bytes",
		Help: "Approximate total size of original avatar files stored in S3.",
	})
)

// --- Инфраструктурные метрики ---

var (
	// DBPoolConnections — состояние пула соединений с PostgreSQL
	// (label "state": idle/used/max), обновляется периодически из
	// pgxpool.Stat() — см. CollectDBPoolStats в dbstats.go.
	DBPoolConnections = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gophprofile_db_pool_connections",
		Help: "Current state of the PostgreSQL connection pool.",
	}, []string{"state"})

	// WorkerJobsTotal — сколько событий обработал воркер, по очереди
	// ("upload"/"delete") и итогу ("success"/"failure").
	WorkerJobsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gophprofile_worker_jobs_total",
		Help: "Total number of worker jobs processed, by queue and outcome.",
	}, []string{"queue", "status"})

	// WorkerJobDuration — сколько суммарно (все попытки retry включены)
	// занимает обработка одного события воркером.
	WorkerJobDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "gophprofile_worker_job_duration_seconds",
		Help:    "Duration of a single worker job (including retries) in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"queue"})
)
