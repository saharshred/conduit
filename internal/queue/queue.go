// Package queue wires the normalizer's output to RabbitMQ with a real
// retry-then-dead-letter pattern — not just "publish and hope": a handler
// that fails gets requeued with backoff up to maxRetries times, and only
// after that gives up and lands on the dead-letter queue where a human (or
// the dashboard, Day 5) can look at it.
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/saharshred/conduit/internal/schema"
)

// DialWithRetry connects to RabbitMQ, retrying with backoff instead of
// failing on the first attempt. This matters even with a compose
// healthcheck in front of RabbitMQ: `rabbitmq-diagnostics ping` can
// report healthy a moment before the AMQP listener on 5672 is actually
// accepting connections (the Erlang node is up before the connection
// handler is fully registered) — a real race that killed cmd/ingestor
// outright in CI (connection refused, log.Fatal, container just dies,
// no restart policy). A handful of retries with backoff is the fix, the
// same pattern this project already uses for the mock banks' 429s and
// RabbitMQ's own dead-letter retries.
func DialWithRetry(url string, maxAttempts int) (*amqp.Connection, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		conn, err := amqp.Dial(url)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if attempt < maxAttempts {
			time.Sleep(time.Duration(attempt) * time.Second) // 1s, 2s, 3s, ...
		}
	}
	return nil, fmt.Errorf("queue: dialing rabbitmq after %d attempts: %w", maxAttempts, lastErr)
}

const (
	ExchangeName  = "conduit"
	MainQueue     = "conduit.transactions"
	RetryQueue    = "conduit.transactions.retry"
	DeadQueue     = "conduit.transactions.dead"
	RoutingKey    = "transactions"
	DeadRoutingKy = "transactions.dead"

	retryHeader = "x-conduit-retry-count"
	maxRetries  = 3
	retryDelay  = 2 * time.Second // how long a message sits in the retry queue before returning to the main one
)

// Topology declares the exchange and all three queues. Idempotent — safe
// to call on every startup.
func Topology(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(ExchangeName, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("queue: declaring exchange: %w", err)
	}

	if _, err := ch.QueueDeclare(MainQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("queue: declaring main queue: %w", err)
	}
	if err := ch.QueueBind(MainQueue, RoutingKey, ExchangeName, false, nil); err != nil {
		return fmt.Errorf("queue: binding main queue: %w", err)
	}

	// The retry queue holds a message for retryDelay, then RabbitMQ's own
	// dead-letter mechanism drops it back onto the main queue — a "delayed
	// retry" built entirely out of native TTL + dead-lettering, no timers
	// or extra services required.
	_, err := ch.QueueDeclare(RetryQueue, true, false, false, false, amqp.Table{
		"x-message-ttl":             int32(retryDelay.Milliseconds()),
		"x-dead-letter-exchange":    ExchangeName,
		"x-dead-letter-routing-key": RoutingKey,
	})
	if err != nil {
		return fmt.Errorf("queue: declaring retry queue: %w", err)
	}

	if _, err := ch.QueueDeclare(DeadQueue, true, false, false, false, nil); err != nil {
		return fmt.Errorf("queue: declaring dead-letter queue: %w", err)
	}
	if err := ch.QueueBind(DeadQueue, DeadRoutingKy, ExchangeName, false, nil); err != nil {
		return fmt.Errorf("queue: binding dead-letter queue: %w", err)
	}

	return nil
}

type Publisher struct {
	ch *amqp.Channel
}

func NewPublisher(ch *amqp.Channel) *Publisher {
	return &Publisher{ch: ch}
}

func (p *Publisher) Publish(ctx context.Context, txn schema.Transaction) error {
	buf, err := json.Marshal(txn)
	if err != nil {
		return err
	}
	return p.ch.PublishWithContext(ctx, ExchangeName, RoutingKey, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        buf,
		Headers:     amqp.Table{retryHeader: int32(0)},
		MessageId:   txn.IdempotencyKey(),
	})
}

// Handler processes one transaction. Returning an error triggers the
// retry/dead-letter flow; returning nil acks the message.
type Handler func(ctx context.Context, txn schema.Transaction) error

// Consume runs handler for every message on the main queue, forever (or
// until ctx is done). Failures are requeued via the retry queue up to
// maxRetries times, then routed to the dead-letter queue.
func Consume(ctx context.Context, ch *amqp.Channel, handler Handler) error {
	msgs, err := ch.Consume(MainQueue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("queue: registering consumer: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-msgs:
			if !ok {
				return fmt.Errorf("queue: delivery channel closed")
			}
			handleDelivery(ctx, ch, msg, handler)
		}
	}
}

func handleDelivery(ctx context.Context, ch *amqp.Channel, msg amqp.Delivery, handler Handler) {
	var txn schema.Transaction
	if err := json.Unmarshal(msg.Body, &txn); err != nil {
		// Unparseable message — no point retrying, straight to dead-letter.
		publishDead(ctx, ch, msg, "unmarshal error: "+err.Error())
		msg.Ack(false)
		return
	}

	retryCount := int32(0)
	if v, ok := msg.Headers[retryHeader]; ok {
		if n, ok := v.(int32); ok {
			retryCount = n
		}
	}

	if err := handler(ctx, txn); err != nil {
		if retryCount >= maxRetries {
			publishDead(ctx, ch, msg, fmt.Sprintf("gave up after %d retries: %v", retryCount, err))
			msg.Ack(false)
			return
		}
		requeue(ctx, ch, msg, retryCount+1)
		msg.Ack(false) // we've handed responsibility to the retry queue; ack the original
		return
	}

	msg.Ack(false)
}

func requeue(ctx context.Context, ch *amqp.Channel, msg amqp.Delivery, nextRetryCount int32) {
	_ = ch.PublishWithContext(ctx, "", RetryQueue, false, false, amqp.Publishing{
		ContentType: msg.ContentType,
		Body:        msg.Body,
		Headers:     amqp.Table{retryHeader: nextRetryCount},
		MessageId:   msg.MessageId,
	})
}

func publishDead(ctx context.Context, ch *amqp.Channel, msg amqp.Delivery, reason string) {
	_ = ch.PublishWithContext(ctx, ExchangeName, DeadRoutingKy, false, false, amqp.Publishing{
		ContentType: msg.ContentType,
		Body:        msg.Body,
		Headers:     amqp.Table{"x-conduit-dead-reason": reason},
		MessageId:   msg.MessageId,
	})
}
