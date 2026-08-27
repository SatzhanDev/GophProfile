package broker

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Consumer читает сообщения из одной или нескольких очередей, привязанных
// к общему exchange avatars.exchange.
type Consumer struct {
	conn *amqp.Connection
	ch   *amqp.Channel
}

// NewConsumer подключается к RabbitMQ и объявляет exchange. Объявление
// идемпотентно — если consumer запустится раньше publisher-а (или наоборот),
// повторное объявление с теми же параметрами не будет ошибкой.
func NewConsumer(url string) (*Consumer, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("connect to rabbitmq: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open channel: %w", err)
	}

	if err := ch.ExchangeDeclare(ExchangeName, "topic", true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("declare exchange: %w", err)
	}

	// QoS(prefetch=1): не забирать следующее сообщение, пока не подтверждена
	// обработка текущего. Генерация миниатюр нагружает CPU — не нужно,
	// чтобы одна горутина набрала себе целую пачку сообщений про запас,
	// пока остальные (или другой инстанс воркера) простаивают без работы.
	if err := ch.Qos(1, 0, false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("set qos: %w", err)
	}

	return &Consumer{conn: conn, ch: ch}, nil
}

// DeclareQueue объявляет durable-очередь с именем name, привязывает её
// к avatars.exchange по routingKey и возвращает канал доставок.
// autoAck выключен — вызывающий код сам подтверждает (Ack) или
// отклоняет (Nack) каждое сообщение после обработки.
func (c *Consumer) DeclareQueue(name, routingKey string) (<-chan amqp.Delivery, error) {
	q, err := c.ch.QueueDeclare(name, true, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("declare queue %q: %w", name, err)
	}

	if err := c.ch.QueueBind(q.Name, routingKey, ExchangeName, false, nil); err != nil {
		return nil, fmt.Errorf("bind queue %q: %w", name, err)
	}

	deliveries, err := c.ch.Consume(q.Name, "", false, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("consume queue %q: %w", name, err)
	}

	return deliveries, nil
}

// Close закрывает канал и соединение. Закрытие канала приводит к тому,
// что все каналы доставок, полученные через DeclareQueue, тоже закрываются —
// это сигнал для RunLoop корректно завершить свои циклы чтения.
func (c *Consumer) Close() error {
	if err := c.ch.Close(); err != nil {
		return fmt.Errorf("close channel: %w", err)
	}
	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("close connection: %w", err)
	}
	return nil
}
