package lib

import (
	"bytes"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEchoConcurrent(t *testing.T) {
	// Set the number of concurrent requests
	concurrency := 30000

	// Atomic counter
	var success int32

	// Create a reusable HTTP client
	client := &http.Client{}

	// Launch concurrent requests
	var wg sync.WaitGroup
	wg.Add(concurrency)
	start := time.Now() // Record start time
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			// Generate a request
			req, err := http.NewRequest("POST", "http://localhost:8080/echo", bytes.NewBuffer([]byte("Hello, World!")))
			if err != nil {
				t.Logf("Error: %v", err) // Log error
				return
			}

			// Send the request
			resp, err := client.Do(req)
			if err != nil {
				t.Logf("Error: %v", err) // Log error
				return
			}

			// Read and discard the response body
			_, err = io.ReadAll(resp.Body)
			resp.Body.Close()
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
