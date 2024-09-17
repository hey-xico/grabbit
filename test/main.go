package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"
	
	"github.com/hey-xico/grabbit"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	rabbitMQURL         = "amqp://guest:guest@localhost:6666/"
	messagePublishDelay = 5 * time.Millisecond
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	broker := grabbit.NewBroker(ctx)
	
	// Set a custom logger (optional)
	customLogger := log.New(os.Stdout, "grabbit: ", log.LstdFlags)
	broker.SetLogger(customLogger)
	// Configure TLS
	tlsConfig := &tls.Config{
		InsecureSkipVerify: false,
		// Provide RootCAs, Certificates, etc.
	}
	
	// Set the AMQP configuration with TLS
	amqpConfig := amqp.Config{
		TLSClientConfig: tlsConfig,
		// Other configurations like SASL mechanisms can be set here
	}
	broker.SetBackoffConfig(grabbit.BackoffConfig{
		InitialInterval: 2 * time.Second,
		MaxInterval:     1 * time.Minute,
		Multiplier:      2.0,
	})
	broker.SetConfig(amqpConfig)
	broker.Use(loggingMiddleware, recoveryMiddleware)
	
	setupConsumers(broker)
	
	go func() {
		if err := broker.Start(rabbitMQURL); err != nil {
			log.Fatalf("Failed to start broker: %v", err)
		}
	}()
	
	time.Sleep(1 * time.Second)
	
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	go startPublishing(ctx)
	
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
	
	log.Println("Shutting down broker...")
	cancel()
	
	if err := broker.Shutdown(); err != nil {
		log.Fatalf("Error during shutdown: %v", err)
	}
}

func setupConsumers(broker *grabbit.Broker) {
	broker.Consumer("fanoutConsumerA", fanoutHandler).
		Exchange("fanout_exchange", grabbit.FanoutExchange, grabbit.WithExchangeDurable(true)).
		Queue("a", grabbit.WithQueueExclusive(true), grabbit.WithQueueAutoDelete(true))
	broker.Consumer("fanoutConsumerB", fanoutHandler).
		Exchange("fanout_exchange", grabbit.FanoutExchange, grabbit.WithExchangeDurable(true)).
		Queue("b", grabbit.WithQueueExclusive(true), grabbit.WithQueueAutoDelete(true))
	
	broker.Consumer("directConsumer", directHandler).
		Exchange("direct_exchange", grabbit.DirectExchange, grabbit.WithExchangeDurable(true)).
		Queue("direct_queue", grabbit.WithQueueDurable(true)).
		Binding("my.routing.key").
		ConsumerOptions(grabbit.WithConsumerAutoAck(false)).
		QoS(5)
	
	broker.Consumer("ConsumerA", topicHandler("A")).
		Exchange("topic_exchange", grabbit.TopicExchange, grabbit.WithExchangeDurable(true)).
		Queue("topic_queue_a", grabbit.WithQueueDurable(true)).
		Binding("logs.a")
	
	broker.Consumer("ConsumerB", topicHandler("B")).
		Exchange("topic_exchange", grabbit.TopicExchange, grabbit.WithExchangeDurable(true)).
		Queue("topic_queue_b", grabbit.WithQueueDurable(true)).
		Binding("logs.b")
	
	broker.Consumer("ConsumerAB", topicHandler("AB")).
		Exchange("topic_exchange", grabbit.TopicExchange, grabbit.WithExchangeDurable(true)).
		Queue("topic_queue_ab", grabbit.WithQueueDurable(true)).
		Binding("logs.*")
}

func startPublishing(ctx context.Context) {
	connection, err := amqp.DialConfig(rabbitMQURL, amqp.Config{
		Properties: amqp.Table{
			//	connection name
			"connection_name": "publisher",
		},
	})
	if err != nil {
		log.Fatalf("Error connecting to RabbitMQ: %v", err)
	}
	defer connection.Close()
	
	ticker := time.NewTicker(messagePublishDelay)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := publishMessages(connection); err != nil {
				log.Printf("Error publishing messages: %v", err)
			}
		}
	}
}

func publishMessages(connection *amqp.Connection) error {
	channel, err := connection.Channel()
	if err != nil {
		return fmt.Errorf("error creating channel: %w", err)
	}
	defer channel.Close()
	
	messages := []struct {
		exchange   string
		routingKey string
		body       string
	}{
		{"fanout_exchange", "", "Hello from fanout producer"},
		{"direct_exchange", "my.routing.key", "Hello from direct producer"},
		{"topic_exchange", "logs.a", "Hello from topic producer A"},
		{"topic_exchange", "logs.b", "Hello from topic producer B"},
		{"topic_exchange", "logs.c", "Hello from topic producer C"},
	}
	
	for _, msg := range messages {
		err = channel.Publish(
			msg.exchange,
			msg.routingKey,
			false,
			false,
			amqp.Publishing{
				ContentType: "text/plain",
				Body:        []byte(msg.body),
			},
		)
		if err != nil {
			log.Printf("Error publishing message to exchange '%s': %v", msg.exchange, err)
		}
	}
	return nil
}

func loggingMiddleware(next grabbit.HandlerFunc) grabbit.HandlerFunc {
	return func(ctx *grabbit.Context) error {
		return next(ctx)
	}
}

func recoveryMiddleware(next grabbit.HandlerFunc) grabbit.HandlerFunc {
	return func(ctx *grabbit.Context) (err error) {
		defer func() {
			if r := recover(); r != nil {
				if nackErr := ctx.Nack(false, true); nackErr != nil {
					log.Printf("Error Nacking message: %v", nackErr)
				}
				err = fmt.Errorf("panic recovered: %v", r)
			}
		}()
		err = next(ctx)
		return
	}
}

func fanoutHandler(ctx *grabbit.Context) error {
	//fmt.Printf("Received message: %s\n", string(ctx.Body()))
	
	return nil
}

func directHandler(ctx *grabbit.Context) error {
	//fmt.Printf("Received message: %s\n", string(ctx.Body()))
	return nil
}

func topicHandler(consumerName string) grabbit.HandlerFunc {
	return func(ctx *grabbit.Context) error {
		//fmt.Printf("Received message: %s\n", string(ctx.Body()))
		
		return nil
		
	}
}
