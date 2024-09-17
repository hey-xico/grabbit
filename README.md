# Grabbit

[![Go Reference](https://pkg.go.dev/badge/github.com/hey-xico/grabbit.svg)](https://pkg.go.dev/github.com/hey-xico/grabbit)
[![Go Report Card](https://goreportcard.com/badge/github.com/hey-xico/grabbit)](https://goreportcard.com/report/github.com/hey-xico/grabbit)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Grabbit is a simplified and idiomatic wrapper around the RabbitMQ Go client, making it easier to consume messages using common AMQP patterns. It abstracts the boilerplate code involved in setting up consumers, exchanges, and queues, allowing developers to focus on writing business logic.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Getting Started](#getting-started)
    - [Creating a Broker](#creating-a-broker)
    - [Setting Up a Consumer](#setting-up-a-consumer)
    - [Middleware Support](#middleware-support)
    - [Graceful Shutdown](#graceful-shutdown)
- [Examples](#examples)
- [Advanced Usage](#advanced-usage)
    - [Custom AMQP Configuration](#custom-amqp-configuration)
    - [Monitoring Broker Status](#monitoring-broker-status)
    - [Handling Errors in Handlers](#handling-errors-in-handlers)
- [Documentation](#documentation)
- [Contributing](#contributing)
    - [Coding Guidelines](#coding-guidelines)
- [License](#license)

## Features

- **Easy Configuration**: Simplifies the setup of exchanges, queues, and bindings with functional options.
- **Middleware Support**: Allows adding middleware for reusable message processing logic.
- **Context Integration**: Uses Go's `context.Context` for cancellation and timeouts, facilitating graceful shutdowns.
- **Customizable Error Handling**: Provides hooks to handle errors according to your application's needs.
- **Advanced Connection Settings**: Supports custom AMQP configurations, including TLS settings.
- **Broker State Management**: Manages connection status and provides channels for monitoring.
- **Automatic Reconnection**: Handles reconnections seamlessly in case of connection loss.
- **Flexible Acknowledgments**: Supports both automatic and manual message acknowledgments.

## Installation

To install Grabbit, use `go get`:

```bash
go get github.com/hey-xico/grabbit
```

Ensure your project is using Go modules (a `go.mod` file is present).

## Getting Started

### Creating a Broker

The `Broker` is responsible for managing connections and consumers. You can create a new broker with the application's context:

```go
ctx := context.Background()
broker := grabbit.NewBroker(ctx)
```

Customize the broker by setting an error handler or adjusting the backoff configuration:

```go
broker.SetErrorHandler(func(err error) {
    log.Printf("Broker error: %v", err)
})

broker.SetBackoffConfig(grabbit.BackoffConfig{
    InitialInterval: 2 * time.Second,
    MaxInterval:     1 * time.Minute,
    Multiplier:      1.5,
})
```

### Setting Up a Consumer

Create a consumer by specifying a name and a handler function:

```go
consumer := broker.Consumer("my_consumer", func(ctx *grabbit.Context) error {
    // Process the message
    fmt.Printf("Received message: %s\n", string(ctx.Body()))
    return nil
})
```

Configure the consumer's exchange, queue, and binding:

```go
consumer.
    Exchange("my_exchange", grabbit.DirectExchange, grabbit.WithExchangeDurable(true)).
    Queue("my_queue", grabbit.WithQueueDurable(true)).
    Binding("routing_key")
```

Set consumer options and QoS settings:

```go
consumer.
    ConsumerOptions(grabbit.WithConsumerAutoAck(false)).
    QoS(10) // Prefetch 10 messages
```

### Middleware Support

Grabbit supports middleware functions that can be applied to consumers or the broker:

```go
// Define a middleware
func loggingMiddleware(next grabbit.HandlerFunc) grabbit.HandlerFunc {
    return func(ctx *grabbit.Context) error {
        log.Printf("Processing message: %s", string(ctx.Body()))
        return next(ctx)
    }
}

// Apply middleware to the consumer
consumer.Use(loggingMiddleware)

// Apply middleware to the broker (applies to all consumers)
broker.Use(loggingMiddleware)
```

### Graceful Shutdown

To gracefully shut down the broker and all consumers, call `Shutdown`:

```go
// Start the broker in a separate goroutine
go func() {
    if err := broker.Start("amqp://guest:guest@localhost:5672/"); err != nil {
        log.Fatalf("Broker stopped: %v", err)
    }
}()

// Listen for OS signals for graceful shutdown
signalChan := make(chan os.Signal, 1)
signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

<-signalChan
log.Println("Shutting down broker...")
if err := broker.Shutdown(); err != nil {
    log.Fatalf("Failed to shut down broker: %v", err)
}
```

## Examples

Here's a complete example of setting up a broker and a consumer:

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/hey-xico/grabbit"
)

func main() {
    ctx := context.Background()
    broker := grabbit.NewBroker(ctx)

    broker.SetErrorHandler(func(err error) {
        log.Printf("Broker error: %v", err)
    })

    // Define the message handler
    handler := func(ctx *grabbit.Context) error {
        log.Printf("Received message: %s", string(ctx.Body()))
        return nil
    }

    // Create a consumer
    broker.Consumer("my_consumer", handler).
        Exchange("my_exchange", grabbit.DirectExchange, grabbit.WithExchangeDurable(true)).
        Queue("my_queue", grabbit.WithQueueDurable(true)).
        Binding("routing_key").
        ConsumerOptions(grabbit.WithConsumerAutoAck(false)).
        QoS(10)

    // Start the broker
    go func() {
        if err := broker.Start("amqp://guest:guest@localhost:5672/"); err != nil {
            log.Fatalf("Broker stopped: %v", err)
        }
    }()

    // Wait for termination signal
    signalChan := make(chan os.Signal, 1)
    signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
    <-signalChan

    log.Println("Shutting down broker...")
    if err := broker.Shutdown(); err != nil {
        log.Fatalf("Failed to shut down broker: %v", err)
    }

    log.Println("Broker shut down gracefully.")
}
```

**To run this example:**

1. Ensure RabbitMQ is running on your local machine at `localhost:5672`.
2. Save the code to `main.go`.
3. Run the program:

   ```bash
   go run main.go
   ```

## Advanced Usage

### Custom AMQP Configuration

Set custom AMQP configurations, such as TLS settings:

```go
import (
    "crypto/tls"
    "crypto/x509"
    "io/ioutil"
    "log"

    "github.com/rabbitmq/amqp091-go"
)

// Load client certificate and CA
cert, err := tls.LoadX509KeyPair("client_cert.pem", "client_key.pem")
if err != nil {
    log.Fatalf("Failed to load certificates: %v", err)
}

caCert, err := ioutil.ReadFile("ca_cert.pem")
if err != nil {
    log.Fatalf("Failed to read CA certificate: %v", err)
}

caCertPool := x509.NewCertPool()
caCertPool.AppendCertsFromPEM(caCert)

tlsConfig := &tls.Config{
    Certificates: []tls.Certificate{cert},
    RootCAs:      caCertPool,
}

amqpConfig := amqp.Config{
    TLSClientConfig: tlsConfig,
}

broker.SetConfig(amqpConfig)
```

### Monitoring Broker Status

Monitor the broker's connection status using the `StatusChan` channel:

```go
go func() {
    for status := range broker.StatusChan {
        switch status {
        case grabbit.StatusConnected:
            log.Println("Broker connected")
        case grabbit.StatusDisconnected:
            log.Println("Broker disconnected")
        case grabbit.StatusConnecting:
            log.Println("Broker connecting")
        }
    }
}()
```

### Handling Errors in Handlers

Message handlers can return errors to trigger retries or log issues:

```go
handler := func(ctx *grabbit.Context) error {
    // Process the message
    if err := processMessage(ctx.Body()); err != nil {
        // Optionally, Nack the message to requeue it
        ctx.Nack(false, true)
        return fmt.Errorf("failed to process message: %w", err)
    }
    return nil
}
```

## Documentation

Detailed documentation is available on [pkg.go.dev](https://pkg.go.dev/github.com/hey-xico/grabbit).

Key types and functions:

- [`Broker`](https://pkg.go.dev/github.com/hey-xico/grabbit#Broker): Manages connections and consumers.
- [`Consumer`](https://pkg.go.dev/github.com/hey-xico/grabbit#Consumer): Represents a message consumer.
- [`Context`](https://pkg.go.dev/github.com/hey-xico/grabbit#Context): Provides methods to interact with the incoming message.
- Functional options for configuring exchanges, queues, bindings, consumers, and QoS.

## Contributing

Contributions are welcome! Please follow these steps:

1. **Fork the repository** on GitHub.
2. **Create a new feature branch**:

   ```bash
   git checkout -b feature/my-feature
   ```

3. **Commit your changes** with clear messages:

   ```bash
   git commit -am 'Add new feature'
   ```

4. **Push to the branch**:

   ```bash
   git push origin feature/my-feature
   ```

5. **Create a new Pull Request** on GitHub.

Please ensure that your code adheres to Go conventions and includes appropriate tests.

### Coding Guidelines

- **Go Version**: Target the latest stable Go release.
- **Code Style**: Use `go fmt` for formatting.
- **Documentation**: Update or add comments for any new public APIs.
- **Testing**: Write unit tests for new features and ensure existing tests pass (`go test ./...`).
- **Linting**: Use `golint` and `go vet` to check for issues.
- **Dependencies**: Keep dependencies to a minimum and use Go modules.

## License

Grabbit is released under the [MIT License](https://opensource.org/licenses/MIT).

---

**Need Help?**

If you encounter any issues or have questions, feel free to:

- Open an issue on the [GitHub repository](https://github.com/hey-xico/grabbit/issues).
- Submit a pull request with improvements or bug fixes.

---

**Happy Coding!**

Grabbit aims to simplify your experience with RabbitMQ in Go. By abstracting the complexities of AMQP setup, you can focus on building robust and efficient applications.

