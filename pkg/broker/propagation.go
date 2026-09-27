package broker

import amqp "github.com/rabbitmq/amqp091-go"

// HeaderCarrier адаптирует amqp.Table (map[string]any) под интерфейс
// propagation.TextMapCarrier из OpenTelemetry: тот умеет работать только
// с map[string]string, а заголовки AMQP-сообщений технически map[string]any.
//
// Используется в обе стороны: при публикации (Publisher.Publish) в неё
// внедряется ("Inject") контекст трейсинга текущего запроса, а на стороне
// воркера (internal/worker.RunLoop) он из неё же извлекается ("Extract") —
// так HTTP-запрос и обработка события в отдельном процессе-воркере
// становятся одним трейсом в Jaeger, а не двумя не связанными друг с другом.
type HeaderCarrier amqp.Table

// Get возвращает значение заголовка по ключу или "", если его нет —
// того же требует интерфейс propagation.TextMapCarrier.
func (c HeaderCarrier) Get(key string) string {
	v, ok := c[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// Set записывает значение заголовка по ключу.
func (c HeaderCarrier) Set(key, value string) {
	c[key] = value
}

// Keys возвращает список всех ключей заголовков — propagation.TextMapCarrier
// использует его, чтобы понять, какие заголовки вообще есть в carrier.
func (c HeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
