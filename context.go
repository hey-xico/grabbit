package grabbit

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Context provides methods to interact with the incoming message.
type Context struct {
	context.Context
	Delivery amqp.Delivery
	data     map[string]interface{}
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
func (c *Context) Header(key string) interface{} {
	return c.Delivery.Headers[key]
}

// Set sets a key-value pair in the context.
func (c *Context) Set(key string, value interface{}) {
	if c.data == nil {
		c.data = make(map[string]interface{})
	}
	c.data[key] = value
}

// Get retrieves a value from the context.
func (c *Context) Get(key string) (interface{}, bool) {
	value, exists := c.data[key]
	return value, exists
}
