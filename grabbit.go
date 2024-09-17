// Package grabbit provides a simplified and idiomatic wrapper around the RabbitMQ Go client,
// making it easier to consume messages using common AMQP patterns.
//
// Key features include:
// - Easy configuration of exchanges, queues, and bindings.
// - Middleware support for reusable message processing logic.
// - Context integration for graceful shutdowns.
// - Customizable error handling.
// - Support for advanced connection settings, including TLS.
// - Broker state management and metrics tracking.
package grabbit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	
	amqp "github.com/rabbitmq/amqp091-go"
)

// ExchangeType represents the type of an AMQP exchange.
type ExchangeType string

const (
	// DirectExchange routes messages to queues based on the routing key.
	DirectExchange ExchangeType = "direct"
	// FanoutExchange routes messages to all bound queues, ignoring routing keys.
	FanoutExchange ExchangeType = "fanout"
	// TopicExchange routes messages to queues based on pattern matching.
	TopicExchange ExchangeType = "topic"
	// HeadersExchange routes messages based on matching message headers.
	HeadersExchange ExchangeType = "headers"
)

// ExchangeOptions defines the configuration options for an exchange.
type ExchangeOptions struct {
	Name       string       // Name of the exchange.
	Type       ExchangeType // Type of the exchange.
	Durable    bool         // Survives broker restarts.
	AutoDelete bool         // Deletes when unused.
	Internal   bool         // Internal exchange (used by the broker).
	NoWait     bool         // No wait for server response.
	Args       amqp.Table   // Additional arguments.
}

// QueueOptions defines the configuration options for a queue.
type QueueOptions struct {
	Name       string     // Name of the queue.
	Durable    bool       // Survives broker restarts.
	AutoDelete bool       // Deletes when unused.
	Exclusive  bool       // Exclusive to the connection.
	NoWait     bool       // No wait for server response.
	Args       amqp.Table // Additional arguments.
}

// BindingOptions defines the options for binding a queue to an exchange.
type BindingOptions struct {
	RoutingKey string     // Routing key for binding.
	Headers    amqp.Table // Headers for headers exchange.
	NoWait     bool       // No wait for server response.
	Args       amqp.Table // Additional arguments.
}

// ConsumerOptions defines the options for a consumer.
type ConsumerOptions struct {
	ConsumerTag string     // Identifier for the consumer.
	AutoAck     bool       // Automatic message acknowledgment.
	Exclusive   bool       // Exclusive access to the queue.
	NoLocal     bool       // Do not receive messages published on the same channel.
	NoWait      bool       // No wait for server response.
	Args        amqp.Table // Additional arguments.
}

// QoSOptions defines the Quality of Service options for a consumer.
type QoSOptions struct {
	PrefetchCount int // Messages to prefetch.
	PrefetchSize  int // Size in bytes to prefetch.
}

// HandlerFunc defines the handler function type for processing messages.
type HandlerFunc func(*Context) error

// MiddlewareFunc defines the middleware function type.
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// ErrorHandler is a function type for handling errors.
type ErrorHandler func(error)

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

// Broker represents the message broker that manages connections and consumers.
type Broker struct {
	url           string
	conn          *amqp.Connection
	connMutex     sync.RWMutex
	middlewares   []MiddlewareFunc
	consumers     []*Consumer
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	config        amqp.Config
	backoffConfig BackoffConfig
	errorHandler  ErrorHandler
}

// BackoffConfig defines the configuration for exponential backoff.
type BackoffConfig struct {
	InitialInterval time.Duration // Initial wait time before reconnecting.
	MaxInterval     time.Duration // Maximum wait time between reconnections.
	Multiplier      float64       // Multiplier for exponential backoff.
}

// NewBroker creates a new Broker instance with the provided application context.
// The application context is used to listen for cancellation signals and access global configurations.
func NewBroker(ctx context.Context) *Broker {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	return &Broker{
		middlewares: []MiddlewareFunc{},
		consumers:   []*Consumer{},
		ctx:         ctx,
		cancel:      cancel,
		config:      amqp.Config{}, // Default AMQP config
		backoffConfig: BackoffConfig{
			InitialInterval: 1 * time.Second,
			MaxInterval:     30 * time.Second,
			Multiplier:      2.0,
		},
	}
}

// SetErrorHandler sets the error handler for the broker.
// Users can provide their own error handler to process errors from the broker and consumers.
func (b *Broker) SetErrorHandler(handler ErrorHandler) {
	b.errorHandler = handler
}

// SetConfig sets the AMQP configuration for the broker.
// This allows users to customize connection settings, including TLS.
func (b *Broker) SetConfig(config amqp.Config) {
	b.config = config
}

// SetBackoffConfig sets the backoff configuration for reconnections.
func (b *Broker) SetBackoffConfig(config BackoffConfig) {
	b.backoffConfig = config
}

// Use adds middleware(s) to the Broker.
// Middleware functions will be applied to all consumers managed by the broker.
func (b *Broker) Use(middleware ...MiddlewareFunc) {
	b.middlewares = append(b.middlewares, middleware...)
}

// Consumer creates a new Consumer with the specified name and handler, and adds it to the Broker.
func (b *Broker) Consumer(name string, handler HandlerFunc) *Consumer {
	consumer := &Consumer{
		name:    name,
		handler: handler,
		broker:  b,
	}
	b.consumers = append(b.consumers, consumer)
	return consumer
}

// Start establishes the connection to the RabbitMQ server and starts all consumers.
// It will attempt to reconnect and restart consumers upon connection loss.
// It listens for cancellation signals from the application's context.
func (b *Broker) Start(url string) error {
	b.url = url
	backoffInterval := b.backoffConfig.InitialInterval
	
	for {
		select {
		case <-b.ctx.Done():
			// Application context cancelled, initiate shutdown
			b.closeConnection()
			return b.ctx.Err()
		default:
			// Attempt to connect
			if err := b.connect(); err != nil {
				b.handleError(fmt.Errorf("failed to connect: %w", err))
				backoffInterval = b.nextBackoff(backoffInterval)
				select {
				case <-b.ctx.Done():
					return b.ctx.Err()
				case <-time.After(backoffInterval):
					continue
				}
			}
			
			// Reset backoff interval upon successful connection
			backoffInterval = b.backoffConfig.InitialInterval
			
			// Start consumers
			if err := b.startConsumers(); err != nil {
				b.handleError(fmt.Errorf("failed to start consumers: %w", err))
				b.closeConnection()
				backoffInterval = b.nextBackoff(backoffInterval)
				select {
				case <-b.ctx.Done():
					return b.ctx.Err()
				case <-time.After(backoffInterval):
					continue
				}
			}
			
			// Monitor connection
			connClosed := make(chan *amqp.Error, 1)
			b.conn.NotifyClose(connClosed)
			
			select {
			case <-b.ctx.Done():
				// Application context cancelled, initiate shutdown
				b.closeConnection()
				return b.ctx.Err()
			case err := <-connClosed:
				if err != nil {
					b.handleError(fmt.Errorf("connection closed: %w", err))
				}
				b.closeConnection()
				// Wait for consumers to stop before reconnecting
				b.wg.Wait()
				backoffInterval = b.nextBackoff(backoffInterval)
				select {
				case <-b.ctx.Done():
					return b.ctx.Err()
				case <-time.After(backoffInterval):
					continue
				}
			}
		}
	}
}

// nextBackoff calculates the next backoff interval.
func (b *Broker) nextBackoff(currentInterval time.Duration) time.Duration {
	nextInterval := time.Duration(float64(currentInterval) * b.backoffConfig.Multiplier)
	if nextInterval > b.backoffConfig.MaxInterval {
		return b.backoffConfig.MaxInterval
	}
	return nextInterval
}

// connect establishes a new connection to the RabbitMQ server.
func (b *Broker) connect() error {
	conn, err := amqp.DialConfig(b.url, b.config)
	if err != nil {
		return fmt.Errorf("failed to dial AMQP server: %w", err)
	}
	b.connMutex.Lock()
	b.conn = conn
	b.connMutex.Unlock()
	return nil
}

// startConsumers initializes and starts all consumers managed by the broker.
func (b *Broker) startConsumers() error {
	// Validate configurations
	for _, consumer := range b.consumers {
		if err := consumer.validateConfig(); err != nil {
			return fmt.Errorf("consumer '%s' configuration error: %w", consumer.name, err)
		}
	}
	
	for _, consumer := range b.consumers {
		b.wg.Add(1)
		go func(c *Consumer) {
			defer b.wg.Done()
			if err := c.start(); err != nil {
				b.handleError(fmt.Errorf("consumer '%s' stopped: %w", c.name, err))
			}
		}(consumer)
	}
	return nil
}

// closeConnection closes the AMQP connection.
func (b *Broker) closeConnection() {
	b.connMutex.Lock()
	defer b.connMutex.Unlock()
	if b.conn != nil {
		_ = b.conn.Close() // Ignoring error since connection is closing
		b.conn = nil
	}
}

// Shutdown gracefully shuts down the broker and all its consumers.
// It cancels the broker's context, triggering cancellation signals.
func (b *Broker) Shutdown() error {
	b.cancel()
	b.wg.Wait()
	b.closeConnection()
	return nil
}

// getConnection safely retrieves the current AMQP connection.
func (b *Broker) getConnection() (*amqp.Connection, error) {
	b.connMutex.RLock()
	defer b.connMutex.RUnlock()
	
	if b.conn == nil {
		return nil, errors.New("connection is not available")
	}
	
	return b.conn, nil
}

// handleError processes errors using the user-provided error handler.
func (b *Broker) handleError(err error) {
	if b.errorHandler != nil {
		b.errorHandler(err)
	}
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

// Use adds middleware(s) to the Consumer.
// Middleware functions will be applied to the consumer's handler.
func (c *Consumer) Use(middleware ...MiddlewareFunc) *Consumer {
	c.middlewares = append(c.middlewares, middleware...)
	return c
}

// validateConfig validates the consumer's configuration before starting.
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

// Context provides methods to interact with the incoming message.
type Context struct {
	context.Context
	Delivery amqp.Delivery
	data     map[string]any
}

// Ack acknowledges the message, indicating successful processing.
// If multiple is true, all messages up to this delivery tag are acknowledged.
func (c *Context) Ack(multiple bool) error {
	return c.Delivery.Ack(multiple)
}

// Nack negatively acknowledges the message, indicating unsuccessful processing.
// If requeue is true, the message will be requeued.
// If multiple is true, multiple messages are negatively acknowledged.
func (c *Context) Nack(multiple, requeue bool) error {
	return c.Delivery.Nack(multiple, requeue)
}

// Reject negatively acknowledges the message without the possibility of requeueing multiple messages.
// If requeue is true, the server will attempt to requeue the message.
func (c *Context) Reject(requeue bool) error {
	return c.Delivery.Reject(requeue)
}

// Body returns the message body.
func (c *Context) Body() []byte {
	return c.Delivery.Body
}

// Header returns the value of the specified header from the message.
func (c *Context) Header(key string) any {
	return c.Delivery.Headers[key]
}

// Set sets a key-value pair in the context.
func (c *Context) Set(key string, value any) {
	if c.data == nil {
		c.data = make(map[string]any)
	}
	c.data[key] = value
}

// Get retrieves a value from the context.
func (c *Context) Get(key string) (any, bool) {
	value, exists := c.data[key]
	return value, exists
}

// Functional options for ExchangeOptions

// WithExchangeDurable sets the Durable option for ExchangeOptions.
func WithExchangeDurable(durable bool) func(*ExchangeOptions) {
	return func(opts *ExchangeOptions) {
		opts.Durable = durable
	}
}

// WithExchangeAutoDelete sets the AutoDelete option for ExchangeOptions.
func WithExchangeAutoDelete(autoDelete bool) func(*ExchangeOptions) {
	return func(opts *ExchangeOptions) {
		opts.AutoDelete = autoDelete
	}
}

// WithExchangeInternal sets the Internal option for ExchangeOptions.
func WithExchangeInternal(internal bool) func(*ExchangeOptions) {
	return func(opts *ExchangeOptions) {
		opts.Internal = internal
	}
}

// WithExchangeNoWait sets the NoWait option for ExchangeOptions.
func WithExchangeNoWait(noWait bool) func(*ExchangeOptions) {
	return func(opts *ExchangeOptions) {
		opts.NoWait = noWait
	}
}

// WithExchangeArgs sets the Args option for ExchangeOptions.
func WithExchangeArgs(args amqp.Table) func(*ExchangeOptions) {
	return func(opts *ExchangeOptions) {
		opts.Args = args
	}
}

// Functional options for QueueOptions

// WithQueueDurable sets the Durable option for QueueOptions.
func WithQueueDurable(durable bool) func(*QueueOptions) {
	return func(opts *QueueOptions) {
		opts.Durable = durable
	}
}

// WithQueueExclusive sets the Exclusive option for QueueOptions.
func WithQueueExclusive(exclusive bool) func(*QueueOptions) {
	return func(opts *QueueOptions) {
		opts.Exclusive = exclusive
	}
}

// WithQueueAutoDelete sets the AutoDelete option for QueueOptions.
func WithQueueAutoDelete(autoDelete bool) func(*QueueOptions) {
	return func(opts *QueueOptions) {
		opts.AutoDelete = autoDelete
	}
}

// WithQueueNoWait sets the NoWait option for QueueOptions.
func WithQueueNoWait(noWait bool) func(*QueueOptions) {
	return func(opts *QueueOptions) {
		opts.NoWait = noWait
	}
}

// WithQueueArgs sets the Args option for QueueOptions.
func WithQueueArgs(args amqp.Table) func(*QueueOptions) {
	return func(opts *QueueOptions) {
		opts.Args = args
	}
}

// WithQueueDeadLetterExchange sets the DeadLetterExchange option for QueueOptions.
func WithQueueDeadLetterExchange(exchange string) func(*QueueOptions) {
	return func(opts *QueueOptions) {
		if opts.Args == nil {
			opts.Args = amqp.Table{}
		}
		opts.Args["x-dead-letter-exchange"] = exchange
	}
}

// WithQueueMaxRetries sets the MaxRetries option for QueueOptions.
func WithQueueMaxRetries(max int) func(*QueueOptions) {
	return func(opts *QueueOptions) {
		if opts.Args == nil {
			opts.Args = amqp.Table{}
		}
		opts.Args["x-max-length"] = max
	}
}

// Functional options for BindingOptions

// WithBindingHeaders sets the Headers option for BindingOptions.
func WithBindingHeaders(headers amqp.Table) func(*BindingOptions) {
	return func(opts *BindingOptions) {
		opts.Headers = headers
	}
}

// WithBindingNoWait sets the NoWait option for BindingOptions.
func WithBindingNoWait(noWait bool) func(*BindingOptions) {
	return func(opts *BindingOptions) {
		opts.NoWait = noWait
	}
}

// WithBindingArgs sets the Args option for BindingOptions.
func WithBindingArgs(args amqp.Table) func(*BindingOptions) {
	return func(opts *BindingOptions) {
		opts.Args = args
	}
}

// Functional options for ConsumerOptions

// WithConsumerTag sets the ConsumerTag option for ConsumerOptions.
func WithConsumerTag(tag string) func(*ConsumerOptions) {
	return func(opts *ConsumerOptions) {
		opts.ConsumerTag = tag
	}
}

// WithConsumerAutoAck sets the AutoAck option for ConsumerOptions.
func WithConsumerAutoAck(autoAck bool) func(*ConsumerOptions) {
	return func(opts *ConsumerOptions) {
		opts.AutoAck = autoAck
	}
}

// WithConsumerExclusive sets the Exclusive option for ConsumerOptions.
func WithConsumerExclusive(exclusive bool) func(*ConsumerOptions) {
	return func(opts *ConsumerOptions) {
		opts.Exclusive = exclusive
	}
}

// WithConsumerNoLocal sets the NoLocal option for ConsumerOptions.
func WithConsumerNoLocal(noLocal bool) func(*ConsumerOptions) {
	return func(opts *ConsumerOptions) {
		opts.NoLocal = noLocal
	}
}

// WithConsumerNoWait sets the NoWait option for ConsumerOptions.
func WithConsumerNoWait(noWait bool) func(*ConsumerOptions) {
	return func(opts *ConsumerOptions) {
		opts.NoWait = noWait
	}
}

// WithConsumerArgs sets the Args option for ConsumerOptions.
func WithConsumerArgs(args amqp.Table) func(*ConsumerOptions) {
	return func(opts *ConsumerOptions) {
		opts.Args = args
	}
}

// Functional options for QoSOptions

// WithQoSPrefetchSize sets the PrefetchSize option for QoSOptions.
func WithQoSPrefetchSize(prefetchSize int) func(*QoSOptions) {
	return func(opts *QoSOptions) {
		opts.PrefetchSize = prefetchSize
	}
}
