package grabbit

import (
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Consumer represents a message consumer with its configurations.
type Consumer struct {
	exchangeOpts ExchangeOptions
	queueOpts    QueueOptions
	bindingOpts  BindingOptions
	consumerOpts ConsumerOptions
	qosOpts      QoSOptions
	handler      HandlerFunc
	broker       *Broker
	middlewares  []MiddlewareFunc
	name         string
}

// Use adds middleware(s) to the Consumer.
// Middleware functions will be applied to the consumer's handler.
func (c *Consumer) Use(middleware ...MiddlewareFunc) *Consumer {
	c.middlewares = append(c.middlewares, middleware...)
	return c
}

// Exchange configures the exchange for the consumer.
func (c *Consumer) Exchange(name string, exchangeType ExchangeType, opts ...func(*ExchangeOptions)) *Consumer {
	c.exchangeOpts = ExchangeOptions{
		Name: name,
		Type: exchangeType,
	}
	for _, opt := range opts {
		opt(&c.exchangeOpts)
	}
	return c
}

// Queue configures the queue for the consumer.
func (c *Consumer) Queue(name string, opts ...func(*QueueOptions)) *Consumer {
	c.queueOpts = QueueOptions{
		Name: name,
	}
	for _, opt := range opts {
		opt(&c.queueOpts)
	}
	return c
}

// Binding configures the binding options for the consumer.
func (c *Consumer) Binding(routingKey string, opts ...func(*BindingOptions)) *Consumer {
	c.bindingOpts = BindingOptions{
		RoutingKey: routingKey,
	}
	for _, opt := range opts {
		opt(&c.bindingOpts)
	}
	return c
}

// ConsumerOptions sets the consumer options.
func (c *Consumer) ConsumerOptions(opts ...func(*ConsumerOptions)) *Consumer {
	for _, opt := range opts {
		opt(&c.consumerOpts)
	}
	return c
}

// QoS sets the QoS options for the consumer.
func (c *Consumer) QoS(prefetchCount int, opts ...func(*QoSOptions)) *Consumer {
	c.qosOpts.PrefetchCount = prefetchCount
	for _, opt := range opts {
		opt(&c.qosOpts)
	}
	return c
}

// Internal methods handle the consumer's internal operations.

func (c *Consumer) start() error {
	conn, err := c.broker.getConnection()
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}

	channel, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open channel: %w", err)
	}
	defer func() {
		_ = channel.Close() // Ignoring error since channel is closing
	}()

	chClosed := make(chan *amqp.Error, 1)
	channel.NotifyClose(chClosed)

	if c.exchangeOpts.Name != "" {
		err = channel.ExchangeDeclare(
			c.exchangeOpts.Name,
			string(c.exchangeOpts.Type),
			c.exchangeOpts.Durable,
			c.exchangeOpts.AutoDelete,
			c.exchangeOpts.Internal,
			c.exchangeOpts.NoWait,
			c.exchangeOpts.Args,
		)
		if err != nil {
			return fmt.Errorf("failed to declare exchange '%s': %w", c.exchangeOpts.Name, err)
		}
	}

	queueName := c.queueOpts.Name
	if queueName == "" {
		queueName = c.name
	}
	queue, err := channel.QueueDeclare(
		queueName,
		c.queueOpts.Durable,
		c.queueOpts.AutoDelete,
		c.queueOpts.Exclusive,
		c.queueOpts.NoWait,
		c.queueOpts.Args,
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue '%s': %w", queueName, err)
	}

	if c.exchangeOpts.Name != "" {
		err = channel.QueueBind(
			queue.Name,
			c.bindingOpts.RoutingKey,
			c.exchangeOpts.Name,
			c.bindingOpts.NoWait,
			c.bindingOpts.Args,
		)
		if err != nil {
			return fmt.Errorf("failed to bind queue '%s' to exchange '%s': %w", queue.Name, c.exchangeOpts.Name, err)
		}
	}

	if c.qosOpts.PrefetchCount > 0 {
		err = channel.Qos(
			c.qosOpts.PrefetchCount,
			c.qosOpts.PrefetchSize,
			false,
		)
		if err != nil {
			return fmt.Errorf("failed to set QoS: %w", err)
		}
	}

	// Start consuming messages
	deliveries, err := channel.Consume(
		queue.Name,
		c.consumerOpts.ConsumerTag,
		c.consumerOpts.AutoAck,
		c.consumerOpts.Exclusive,
		c.consumerOpts.NoLocal,
		c.consumerOpts.NoWait,
		c.consumerOpts.Args,
	)
	if err != nil {
		return fmt.Errorf("failed to start consuming from queue '%s': %w", queueName, err)
	}

	handler := c.applyMiddleware(c.handler)

	for {
		select {
		case <-c.broker.ctx.Done():
			return nil
		case err, ok := <-chClosed:
			if !ok || err == nil {
				return errors.New("channel closed unexpectedly")
			}
			return fmt.Errorf("channel closed: %w", err)
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("deliveries channel closed")
			}

			ctx := &Context{
				Context:  c.broker.ctx,
				Delivery: d,
			}
			if err := handler(ctx); err != nil {
				if !c.consumerOpts.AutoAck {
					if nackErr := ctx.Nack(false, true); nackErr != nil {
						return fmt.Errorf("failed to Nack message: %w", nackErr)
					}
				}
				// Return handler error
				return fmt.Errorf("handler error: %w", err)
			} else {
				if !c.consumerOpts.AutoAck {
					if ackErr := ctx.Ack(false); ackErr != nil {
						return fmt.Errorf("failed to Ack message: %w", ackErr)
					}
				}
			}
		}
	}
}

func (c *Consumer) validateConfig() error {
	validExchangeTypes := map[ExchangeType]bool{
		DirectExchange:  true,
		FanoutExchange:  true,
		TopicExchange:   true,
		HeadersExchange: true,
	}
	// Validate Exchange Options
	if c.exchangeOpts.Name != "" {
		if !validExchangeTypes[c.exchangeOpts.Type] {
			return fmt.Errorf("invalid exchange type '%s' for exchange '%s'", c.exchangeOpts.Type, c.exchangeOpts.Name)
		}
		if c.exchangeOpts.Name == "" {
			return fmt.Errorf("exchange name cannot be empty")
		}
	}

	// Validate Queue Options
	if c.queueOpts.Name == "" && c.exchangeOpts.Name == "" {
		return fmt.Errorf("queue name cannot be empty unless bound to an exchange")
	}

	// Validate Binding Options
	if c.exchangeOpts.Name != "" {
		if c.exchangeOpts.Type == HeadersExchange && len(c.bindingOpts.Headers) == 0 {
			return fmt.Errorf("headers exchange requires binding headers")
		}
		if c.exchangeOpts.Type != HeadersExchange && c.exchangeOpts.Type != FanoutExchange && c.bindingOpts.RoutingKey == "" {
			return fmt.Errorf("routing key cannot be empty for exchange type '%s'", c.exchangeOpts.Type)
		}
	}

	return nil
}

func (c *Consumer) applyMiddleware(h HandlerFunc) HandlerFunc {
	// Apply broker middlewares first
	for i := len(c.broker.middlewares) - 1; i >= 0; i-- {
		h = c.broker.middlewares[i](h)
	}
	// Then apply consumer-specific middlewares
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		h = c.middlewares[i](h)
	}
	return h
}
