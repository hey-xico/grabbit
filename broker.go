package grabbit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
	
	amqp "github.com/rabbitmq/amqp091-go"
)

// Broker manages connections and consumers for RabbitMQ.
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
	
	status      BrokerStatus
	statusMutex sync.RWMutex
	StatusChan  chan BrokerStatus
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
		status:     StatusDisconnected,
		StatusChan: make(chan BrokerStatus, 1),
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
			// Application context canceled, initiate shutdown
			b.closeConnection()
			return b.ctx.Err()
		default:
			b.setStatus(StatusConnecting)
			
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
			
			b.setStatus(StatusConnected)
			
			// Start consumers
			if err := b.startConsumers(); err != nil {
				b.handleError(fmt.Errorf("failed to start consumers: %w", err))
				b.closeConnection()
				backoffInterval = b.nextBackoff(backoffInterval)
				b.setStatus(StatusDisconnected)
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
				// Application context canceled, initiate shutdown
				b.closeConnection()
				return b.ctx.Err()
			case err := <-connClosed:
				if err != nil {
					b.handleError(fmt.Errorf("connection closed: %w", err))
				}
				b.setStatus(StatusDisconnected)
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

// Shutdown gracefully shuts down the broker and all its consumers.
// It cancels the broker's context, triggering cancellation signals.
func (b *Broker) Shutdown() error {
	b.cancel()
	b.wg.Wait()
	b.closeConnection()
	return nil
}

// GetStatus returns the current status of the broker.
func (b *Broker) GetStatus() BrokerStatus {
	b.statusMutex.RLock()
	defer b.statusMutex.RUnlock()
	return b.status
}

// Internal methods below are unexported and handle the broker's internal operations.

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

func (b *Broker) closeConnection() {
	b.connMutex.Lock()
	defer b.connMutex.Unlock()
	if b.conn != nil {
		_ = b.conn.Close()
		b.conn = nil
	}
}

func (b *Broker) getConnection() (*amqp.Connection, error) {
	b.connMutex.RLock()
	defer b.connMutex.RUnlock()
	
	if b.conn == nil {
		return nil, errors.New("connection is not available")
	}
	
	return b.conn, nil
}

func (b *Broker) handleError(err error) {
	if b.errorHandler != nil {
		b.errorHandler(err)
	}
}

func (b *Broker) nextBackoff(currentInterval time.Duration) time.Duration {
	nextInterval := time.Duration(float64(currentInterval) * b.backoffConfig.Multiplier)
	if nextInterval > b.backoffConfig.MaxInterval {
		return b.backoffConfig.MaxInterval
	}
	return nextInterval
}

func (b *Broker) setStatus(status BrokerStatus) {
	b.statusMutex.Lock()
	b.status = status
	b.statusMutex.Unlock()
	
	// Non-blocking status update to prevent blocking the broker
	select {
	case b.StatusChan <- status:
	default:
		select {
		case <-b.StatusChan: // Remove the old status
		default:
		}
		
		select {
		case b.StatusChan <- status:
		default:
		}
	}
}
