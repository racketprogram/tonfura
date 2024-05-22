package lib

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/go-redis/redis/v8"
)

var ctx = context.Background()

func TestRedis(t *testing.T) {
	// Create a Redis connection
	rdb := redis.NewClient(&redis.Options{
		Addr:        "localhost:6379", // Redis server address
		DB:          0,                // Use default database
	})

	// Flush all data in Redis
	err := rdb.FlushAll(ctx).Err()
	if err != nil {
		fmt.Println("Failed to flush Redis:", err)
		return
	}

	for i := 0; i < 500; i++ {
		// Add user to the set of appointments
		err := rdb.SAdd(ctx, "appointments", fmt.Sprintf("%v", i)).Err()
		if err != nil {
			return
		}
	}

	size, err := rdb.SCard(ctx, "appointments").Result()
	if err != nil {
		fmt.Println("Failed to get set size:", err)
		t.Fail()
	}

	// Convert size to string
	available := int(int(size) / 5)

	// Save size to Redis
	err = rdb.Set(ctx, "available", available, 0).Err()
	if err != nil {
		fmt.Println("Failed to save set size:", err)
		t.Fail()
	}

	// Test reading from Redis
	val, err := rdb.Get(ctx, "available").Int()
	if err != nil {
		t.Errorf("Failed to get value from Redis: %v", err)
	}

	fmt.Println(val)

	// Verify that the read value is correct
	if val != available {
		t.Errorf("Expected value '%d', got '%d'", available, val)
	}

	// Set concurrency number
	numThreads := 10
	// Set the amount to decrement for each Goroutine
	decrementAmount := 1
	// Set the expected result
	expectedValue := val - (numThreads * decrementAmount)

	// Create a WaitGroup to wait for all Goroutines to complete
	var wg sync.WaitGroup
	wg.Add(numThreads)

	// Launch numThreads Goroutines to perform concurrent DECR operations
	for i := 0; i < numThreads; i++ {
		go func() {
			defer wg.Done()
			// Each Goroutine performs a DECR operation
			// Perform DECRBY operation and get the result
			newValue, err := rdb.DecrBy(ctx, "available", int64(decrementAmount)).Result()
			if err != nil {
				t.Errorf("Failed to decrement value: %v", err)
				return
			}
			_ = newValue
			// fmt.Println(newValue)
		}()
	}

	// Wait for all Goroutines to complete
	wg.Wait()

	// Check if the final value matches the expected value
	finalValue, err := rdb.Get(ctx, "available").Int()
	if err != nil {
		t.Errorf("Failed to get final value from Redis: %v", err)
	}

	if finalValue != expectedValue {
		t.Errorf("Expected final value %d, got %d", expectedValue, finalValue)
	}

	fmt.Println(finalValue)
}
