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
	// Name is the name of the exchange.
	Name string
	// Type is the type of the exchange (e.g., direct, fanout, topic, headers).
	Type ExchangeType
	// Durable indicates whether the exchange survives broker restarts.
	Durable bool
	// AutoDelete indicates whether the exchange is deleted when unused.
	AutoDelete bool
	// Internal indicates whether the exchange is internal (used by the broker).
	Internal bool
	// NoWait indicates that the server should not wait for the exchange declaration.
	NoWait bool
	// Args are additional arguments for the exchange declaration.
	Args amqp.Table
}

// QueueOptions defines the configuration options for a queue.
type QueueOptions struct {
	// Name is the name of the queue.
	Name string
	// Durable indicates whether the queue survives broker restarts.
	Durable bool
	// AutoDelete indicates whether the queue is deleted when unused.
	AutoDelete bool
	// Exclusive indicates whether the queue is exclusive to the connection.
	Exclusive bool
	// NoWait indicates that the server should not wait for the queue declaration.
	NoWait bool
	// Args are additional arguments for the queue declaration.
	Args amqp.Table
}

// BindingOptions defines the options for binding a queue to an exchange.
type BindingOptions struct {
	// RoutingKey is the routing key for binding.
	RoutingKey string
	// Headers are the headers for a headers exchange.
	Headers amqp.Table
	// NoWait indicates that the server should not wait for the binding.
	NoWait bool
	// Args are additional arguments for the binding.
	Args amqp.Table
}

// ConsumerOptions defines the options for a consumer.
type ConsumerOptions struct {
	// ConsumerTag is the identifier for the consumer.
	ConsumerTag string
	// AutoAck indicates whether messages are automatically acknowledged.
	AutoAck bool
	// Exclusive indicates whether the consumer has exclusive access to the queue.
	Exclusive bool
	// NoLocal indicates that the server should not deliver messages published on the same channel.
	NoLocal bool
	// NoWait indicates that the server should not wait for the consumer to be registered.
	NoWait bool
	// Args are additional arguments for the consumer.
	Args amqp.Table
}

// QoSOptions defines the Quality of Service options for a consumer.
type QoSOptions struct {
	// PrefetchCount is the number of messages to prefetch.
	PrefetchCount int
	// PrefetchSize is the number of bytes to prefetch.
	PrefetchSize int
}

// HandlerFunc defines the handler function type for processing messages.
type HandlerFunc func(*Context) error

// MiddlewareFunc defines the middleware function type.
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// ErrorHandler is a function type for handling errors.
type ErrorHandler func(error)

// BrokerStatus represents the connection status of the broker.
type BrokerStatus int

const (
	// StatusDisconnected indicates that the broker is disconnected.
	StatusDisconnected BrokerStatus = iota
	// StatusConnecting indicates that the broker is attempting to connect.
	StatusConnecting
	// StatusConnected indicates that the broker is connected.
	StatusConnected
)

// BackoffConfig defines the configuration for exponential backoff.
type BackoffConfig struct {
	// InitialInterval is the initial wait time before reconnecting.
	InitialInterval time.Duration
	// MaxInterval is the maximum wait time between reconnections.
	MaxInterval time.Duration
	// Multiplier is the multiplier for exponential backoff.
	Multiplier float64
}
