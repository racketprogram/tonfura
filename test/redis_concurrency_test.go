package lib

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
)

func TestRedisConcurrentDecr(t *testing.T) {
	// Initialize Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // Add password if any
		DB:       0,  // Select default database
		// PoolSize: 100,
		// PoolTimeout: time.Second,
	})

	// Prepare context
	ctx := context.Background()

	initValue := 100000

	// Set initial value
	if err := rdb.Set(ctx, "available", initValue, 0).Err(); err != nil {
		log.Fatalln("failed to save set size:", err)
	}

	// Set concurrency
	concurrency := 30000

	// Atomic counter
	var success int32

	// Launch concurrent requests
	var wg sync.WaitGroup
	wg.Add(concurrency)
	start := time.Now() // Record start time
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			// Call your function here and pass necessary parameters
			newValue, err := rdb.Decr(ctx, "available").Result()
			if err != nil {
				t.Logf("Error: %v", err) // Log error
				return
			}
			// Update success count using atomic operation
			atomic.AddInt32(&success, 1)
			_ = newValue
		}()
	}

	// Get the final value from Redis
	finalValue, err := rdb.Get(ctx, "available").Int()
	if err != nil {
		t.Errorf("Failed to get final value from Redis: %v", err)
	}
	// Calculate the expected final value
	expectedValue := initValue - concurrency
	// Assert that the final value is as expected
	if finalValue != expectedValue {
		t.Errorf("Expected final value %d, got %d", expectedValue, finalValue)
	}

	// Wait for all concurrent requests to complete
	wg.Wait()
	elapsed := time.Since(start)           // Calculate test duration
	t.Logf("Total time: %v", elapsed)      // Log total time
	t.Logf("Success request: %d", success) // Log successful requests count
}
