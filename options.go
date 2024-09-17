package grabbit

import (
	amqp "github.com/rabbitmq/amqp091-go"
)

// Functional options for configuring exchanges, queues, bindings, consumers, and QoS.

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

// WithQueueDeadLetterExchange sets the dead-letter exchange for the queue.
func WithQueueDeadLetterExchange(exchange string) func(*QueueOptions) {
	return func(opts *QueueOptions) {
		if opts.Args == nil {
			opts.Args = amqp.Table{}
		}
		opts.Args["x-dead-letter-exchange"] = exchange
	}
}

// WithQueueMaxRetries sets the maximum number of retries for the queue.
func WithQueueMaxRetries(max int) func(*QueueOptions) {
	return func(opts *QueueOptions) {
		if opts.Args == nil {
			opts.Args = amqp.Table{}
		}
		opts.Args["x-max-length"] = max
	}
}

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

// WithQoSPrefetchSize sets the PrefetchSize option for QoSOptions.
func WithQoSPrefetchSize(prefetchSize int) func(*QoSOptions) {
	return func(opts *QoSOptions) {
		opts.PrefetchSize = prefetchSize
	}
}
