package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQ struct {
	url       string
	queueName string
}

func NewRabbitMQ(url, queueName string) *RabbitMQ {
	return &RabbitMQ{url: url, queueName: queueName}
}

func (r *RabbitMQ) PublishJSON(ctx context.Context, body any) error {
	conn, err := amqp.Dial(r.url)
	if err != nil {
		return fmt.Errorf("rabbitmq dial: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("rabbitmq channel: %w", err)
	}
	defer ch.Close()

	_, err = ch.QueueDeclare(
		r.queueName,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,
	)
	if err != nil {
		return fmt.Errorf("rabbitmq declare queue: %w", err)
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	pubCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return ch.PublishWithContext(
		pubCtx,
		"", // default exchange
		r.queueName,
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         data,
			Timestamp:    time.Now(),
		},
	)
}

func (r *RabbitMQ) Consume(ctx context.Context, consumerName string, prefetch int) (<-chan amqp.Delivery, func() error, error) {
	conn, err := amqp.Dial(r.url)
	if err != nil {
		return nil, nil, fmt.Errorf("rabbitmq dial: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("rabbitmq channel: %w", err)
	}

	_, err = ch.QueueDeclare(
		r.queueName,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, nil, fmt.Errorf("rabbitmq declare queue: %w", err)
	}

	if prefetch > 0 {
		if err := ch.Qos(prefetch, 0, false); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return nil, nil, fmt.Errorf("rabbitmq qos: %w", err)
		}
	}

	deliveries, err := ch.ConsumeWithContext(
		ctx,
		r.queueName,
		consumerName,
		false, // autoAck
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, nil, fmt.Errorf("rabbitmq consume: %w", err)
	}

	closeFn := func() error {
		_ = ch.Close()
		return conn.Close()
	}

	return deliveries, closeFn, nil
}

