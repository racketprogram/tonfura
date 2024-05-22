package lib

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

func TestKafkaConcurrentWrite(t *testing.T) {
	// Kafka configuration
	writer := kafka.NewWriter(kafka.WriterConfig{
		Brokers:  []string{"localhost:9092"},
		Topic:    "user_ids",
		Balancer: &kafka.LeastBytes{},
	})
	defer writer.Close()

	// Set concurrency
	concurrency := 1000000

	// Atomic counter
	var success int32

	// Launch concurrent requests
	var wg sync.WaitGroup
	wg.Add(concurrency)
	start := time.Now() // Record start time
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			// Generate a message
			msg := kafka.Message{
				Key:   []byte("key"),
				Value: []byte("value"),
			}
			// Write the message to Kafka
			err := writer.WriteMessages(context.Background(), msg)
			if err != nil {
				t.Logf("Error: %v", err) // Log error
				return
			}
			// Update success count using atomic operation
			atomic.AddInt32(&success, 1)
		}()
	}

	// Wait for all concurrent requests to complete
	wg.Wait()
	elapsed := time.Since(start)           // Calculate test duration
	t.Logf("Total time: %v", elapsed)      // Log total time
	t.Logf("Success request: %d", success) // Log successful requests count
}
