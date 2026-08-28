package api

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

// ipRateLimiter — по одному rate.Limiter (алгоритм token bucket) на
// каждый IP-адрес клиента. requestsPerSecond — скорость пополнения
// "бакета" токенами, burst — сколько запросов можно сделать одним
// всплеском, не дожидаясь пополнения.
//
// Ограничение: это лимит "в один процесс" — если поднять несколько
// реплик server за балансировщиком, каждая будет считать свой лимит
// независимо, и общий лимит на клиента фактически умножится на число
// реплик. Для честного общего лимита на несколько инстансов нужен общий
// стейт (Redis и т.п.) — за рамками текущего MVP. Также карта limiters
// растёт с числом уникальных IP и никогда не очищается — приемлемо для
// учебного проекта, в проде нужна была бы очистка неактивных записей.
type ipRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	r        rate.Limit
	b        int
}

func newIPRateLimiter(requestsPerSecond float64, burst int) *ipRateLimiter {
	return &ipRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		r:        rate.Limit(requestsPerSecond),
		b:        burst,
	}
}

func (l *ipRateLimiter) getLimiter(ip string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	limiter, ok := l.limiters[ip]
	if !ok {
		limiter = rate.NewLimiter(l.r, l.b)
		l.limiters[ip] = limiter
	}

	return limiter
}

// Middleware отклоняет запрос с кодом 429, если клиент с этого IP
// превысил лимит. Ставится после middleware.RealIP в цепочке, поэтому
// r.RemoteAddr к этому моменту уже учитывает X-Forwarded-For/X-Real-IP,
// если сервис стоит за прокси.
func (l *ipRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.getLimiter(clientIP(r)).Allow() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "too many requests"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP достаёт IP без порта из r.RemoteAddr ("1.2.3.4:56789" -> "1.2.3.4").
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
