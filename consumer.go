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

// start initializes the consumer's channel and starts consuming messages.
func (c *Consumer) start() error {
	channel, err := c.setupChannel()
	if err != nil {
		return err
	}
	defer closeChannel(channel)

	chCloseNotify := make(chan *amqp.Error, 1)
	channel.NotifyClose(chCloseNotify)

	if err := c.declareExchange(channel); err != nil {
		return err
	}

	queue, err := c.declareQueue(channel)
	if err != nil {
		return err
	}

	if err := c.bindQueue(channel, queue); err != nil {
		return err
	}

	if err := c.setupQoS(channel); err != nil {
		return err
	}

	deliveries, err := c.startConsuming(channel, queue)
	if err != nil {
		return err
	}

	handler := c.applyMiddleware(c.handler)
	return c.processMessages(deliveries, handler, chCloseNotify)
}

// setupChannel creates and returns a new AMQP channel.
func (c *Consumer) setupChannel() (*amqp.Channel, error) {
	conn, err := c.broker.getConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	channel, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	return channel, nil
}

// closeChannel safely closes the given AMQP channel.
func closeChannel(channel *amqp.Channel) {
	if channel != nil {
		_ = channel.Close()
	}
}

// declareExchange declares the exchange if ExchangeOptions are set.
func (c *Consumer) declareExchange(channel *amqp.Channel) error {
	if c.exchangeOpts.Name == "" {
		return nil
	}

	err := channel.ExchangeDeclare(
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

	return nil
}

// declareQueue declares the queue and returns its details.
func (c *Consumer) declareQueue(channel *amqp.Channel) (amqp.Queue, error) {
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
		return amqp.Queue{}, fmt.Errorf("failed to declare queue '%s': %w", queueName, err)
	}

	return queue, nil
}

// bindQueue binds the queue to the exchange if ExchangeOptions are set.
func (c *Consumer) bindQueue(channel *amqp.Channel, queue amqp.Queue) error {
	if c.exchangeOpts.Name == "" {
		return nil
	}

	err := channel.QueueBind(
		queue.Name,
		c.bindingOpts.RoutingKey,
		c.exchangeOpts.Name,
		c.bindingOpts.NoWait,
		c.bindingOpts.Args,
	)
	if err != nil {
		return fmt.Errorf("failed to bind queue '%s' to exchange '%s': %w", queue.Name, c.exchangeOpts.Name, err)
	}

	return nil
}

// setupQoS configures the Quality of Service settings for the consumer.
func (c *Consumer) setupQoS(channel *amqp.Channel) error {
	if c.qosOpts.PrefetchCount <= 0 && c.qosOpts.PrefetchSize <= 0 {
		return nil
	}

	err := channel.Qos(
		c.qosOpts.PrefetchCount,
		c.qosOpts.PrefetchSize,
		false,
	)
	if err != nil {
		return fmt.Errorf("failed to set QoS: %w", err)
	}

	return nil
}

// startConsuming starts consuming messages from the queue.
func (c *Consumer) startConsuming(channel *amqp.Channel, queue amqp.Queue) (<-chan amqp.Delivery, error) {
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
		return nil, fmt.Errorf("failed to start consuming from queue '%s': %w", queue.Name, err)
	}

	return deliveries, nil
}

// processMessages handles incoming message deliveries.
func (c *Consumer) processMessages(deliveries <-chan amqp.Delivery, handler HandlerFunc, chCloseNotify chan *amqp.Error) error {
	for {
		select {
		case <-c.broker.ctx.Done():
			return nil
		case err, ok := <-chCloseNotify:
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
				return fmt.Errorf("handler error: %w", err)
			}

			if !c.consumerOpts.AutoAck {
				if ackErr := ctx.Ack(false); ackErr != nil {
					return fmt.Errorf("failed to Ack message: %w", ackErr)
				}
			}
		}
	}
}

// validateConfig validates the consumer's configuration before starting.
func (c *Consumer) validateConfig() error {
	if err := c.validateExchange(); err != nil {
		return err
	}

	if err := c.validateQueue(); err != nil {
		return err
	}

	if err := c.validateBinding(); err != nil {
		return err
	}

	return nil
}

// validateExchange checks if the exchange configuration is valid.
func (c *Consumer) validateExchange() error {
	if c.exchangeOpts.Name == "" {
		return nil
	}

	validExchangeTypes := map[ExchangeType]bool{
		DirectExchange:  true,
		FanoutExchange:  true,
		TopicExchange:   true,
		HeadersExchange: true,
	}

	if !validExchangeTypes[c.exchangeOpts.Type] {
		return fmt.Errorf("invalid exchange type '%s' for exchange '%s'", c.exchangeOpts.Type, c.exchangeOpts.Name)
	}

	return nil
}

// validateQueue checks if the queue configuration is valid.
func (c *Consumer) validateQueue() error {
	if c.queueOpts.Name != "" {
		return nil
	}

	if c.exchangeOpts.Name == "" {
		return errors.New("queue name cannot be empty unless bound to an exchange")
	}

	return nil
}

// validateBinding checks if the binding configuration is valid.
func (c *Consumer) validateBinding() error {
	if c.exchangeOpts.Name == "" {
		return nil
	}

	switch c.exchangeOpts.Type {
	case HeadersExchange:
		if len(c.bindingOpts.Headers) == 0 {
			return errors.New("headers exchange requires binding headers")
		}
	case DirectExchange, TopicExchange:
		if c.bindingOpts.RoutingKey == "" {
			return fmt.Errorf("routing key cannot be empty for exchange type '%s'", c.exchangeOpts.Type)
		}
	case FanoutExchange:
		// Fanout exchanges ignore routing keys.
	default:
		return fmt.Errorf("unsupported exchange type '%s'", c.exchangeOpts.Type)
	}

	return nil
}

// applyMiddleware applies the middleware stack to a handler.
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
