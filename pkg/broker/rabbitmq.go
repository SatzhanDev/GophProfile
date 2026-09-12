// Package broker — инфраструктурная обёртка над RabbitMQ. Как и pkg/postgres,
// pkg/s3, здесь нет бизнес-логики — только подключение и отправка сообщений.
// Что именно отправлять и когда — решает internal/services.
package broker

import (
	"context"
	"encoding/json"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ExchangeName — exchange, в который публикуются все события про аватарки.
// Тип "topic" выбран по ТЗ и позволяет в будущем подписываться не только
// на "avatar.uploaded"/"avatar.deleted" целиком, но и по маске "avatar.*".
const ExchangeName = "avatars.exchange"

var tracer = otel.Tracer("rabbitmq")

// Publisher публикует события в RabbitMQ.
type Publisher struct {
	conn *amqp.Connection
	ch   *amqp.Channel
}

// NewPublisher подключается к RabbitMQ и объявляет exchange. Объявление
// идемпотентно: если exchange уже существует с такими же параметрами,
// повторный вызов ExchangeDeclare ничего не сломает — RabbitMQ считает
// такое объявление no-op.
func NewPublisher(url string) (*Publisher, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("connect to rabbitmq: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}

	err = ch.ExchangeDeclare(
		ExchangeName,
		"topic",
		true,  // durable — exchange переживёт перезапуск RabbitMQ
		false, // auto-deleted
		false, // internal
		false, // no-wait
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("declare exchange: %w", err)
	}

	return &Publisher{conn: conn, ch: ch}, nil
}

// Publish сериализует payload в JSON и публикует его с заданным routing key.
//
// MessageId — случайный UUID на каждое сообщение. По ТЗ это нужно для
// идемпотентности: получатель (worker) сможет опознать повторную доставку
// одного и того же сообщения (RabbitMQ иногда доставляет сообщения больше
// одного раза — гарантия "at least once", а не "exactly once").
func (p *Publisher) Publish(ctx context.Context, routingKey string, payload any) error {
	ctx, span := tracer.Start(ctx, "rabbitmq.publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.destination", ExchangeName),
			attribute.String("messaging.rabbitmq.routing_key", routingKey),
		),
	)
	defer span.End()

	body, err := json.Marshal(payload)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("marshal payload: %w", err)
	}

	// Внедряем контекст трейсинга прямо в заголовки сообщения — так worker,
	// забрав это сообщение из очереди (см. internal/worker.RunLoop), сможет
	// продолжить ТОТ ЖЕ трейс, а не начать несвязанный новый. В Jaeger весь
	// путь "HTTP-запрос → публикация в очередь → обработка воркером" будет
	// виден одной цепочкой, хотя это два разных процесса.
	headers := amqp.Table{}
	otel.GetTextMapPropagator().Inject(ctx, HeaderCarrier(headers))

	err = p.ch.PublishWithContext(ctx, ExchangeName, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent, // сообщение переживёт перезапуск RabbitMQ
		MessageId:    uuid.NewString(),
		Headers:      headers,
		Body:         body,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("publish message: %w", err)
	}

	return nil
}

// Ping реализует handlers.Pinger — используется health-чеком.
func (p *Publisher) Ping(_ context.Context) error {
	if p.conn == nil || p.conn.IsClosed() {
		return fmt.Errorf("rabbitmq connection is closed")
	}
	return nil
}

// Close закрывает канал и соединение. Вызывается один раз при остановке приложения.
func (p *Publisher) Close() error {
	if err := p.ch.Close(); err != nil {
		return fmt.Errorf("close channel: %w", err)
	}
	if err := p.conn.Close(); err != nil {
		return fmt.Errorf("close connection: %w", err)
	}
	return nil
}
