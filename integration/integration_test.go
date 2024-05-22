package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Helper function to send POST requests with JSON body
func sendPostRequest(url string, body map[string]interface{}) (*http.Response, error) {
	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	return client.Do(req)
}

func TestIntegration(t *testing.T) {
	// Get startHour and startMinute from environment variables
	startHourStr := os.Getenv("START_HOUR")
	startMinuteStr := os.Getenv("START_MINUTE")

	t.Logf("START_HOUR: %s", startHourStr)
	t.Logf("START_MINUTE: %s", startMinuteStr)

	startHour, err := strconv.Atoi(startHourStr)
	if err != nil {
		t.Fatalf("Failed to parse START_HOUR: %v", err)
	}
	startMinute, err := strconv.Atoi(startMinuteStr)
	if err != nil {
		t.Fatalf("Failed to parse START_MINUTE: %v", err)
	}

	baseURL := "http://localhost:8080"
	numUsers := 30000

	var wg sync.WaitGroup

	// Initialize a counter to track successful appointments
	successCounter := 0

	// Step 1: Concurrently test /appointment endpoint for numUsers users
	for i := 1; i <= numUsers; i++ {
		userID := i
		appointmentBody := map[string]interface{}{
			"userID": userID,
		}
		resp, err := sendPostRequest(fmt.Sprintf("%s/appointment", baseURL), appointmentBody)
		if err != nil {
			t.Errorf("Failed to send POST request for user %d: %v", userID, err)
			return
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := ioutil.ReadAll(resp.Body)
			t.Errorf("Expected status %d for user %d but got %d. Response body: %s", http.StatusOK, userID, resp.StatusCode, string(body))
			return
		}

		// Increase the success counter if the appointment was successful
		successCounter++

		var respBody map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&respBody)
		if err != nil {
			t.Errorf("Failed to decode response body for user %d: %v", userID, err)
			return
		}
		assert.Equal(t, "appointment scheduled successfully", respBody["message"])
	}

	// Step 2: Wait until the specified start time plus one minute
	startTime := time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day(), startHour, startMinute, 0, 0, time.Now().Location())
	for {
		now := time.Now()
		if now.After(startTime.Add(1 * time.Minute)) {
			// If the current time is after the allowed booking time, skip booking
			t.Fatalf("Current time is past the allowed booking time")
			return
		}
		if now.After(startTime.Add(5 * time.Second)) {
			// If the current time is within the allowed booking time, start booking
			break
		}
		// Sleep for a short duration to avoid busy-waiting
		time.Sleep(1 * time.Second)
	}

	success := 0

	// Step 3: Concurrently test /book_coupon endpoint for the same numUsers users
	for i := 1; i <= numUsers; i++ {
		wg.Add(1)
		go func(userID int) {
			defer wg.Done()
			bookCouponBody := map[string]interface{}{
				"userID": userID,
			}
			resp, err := sendPostRequest(fmt.Sprintf("%s/book_coupon", baseURL), bookCouponBody)
			if err != nil {
				t.Errorf("Failed to send POST request for user %d: %v", userID, err)
				return
			}
			if resp.StatusCode != http.StatusOK {
				body, _ := ioutil.ReadAll(resp.Body)
				t.Errorf("Expected status %d for user %d but got %d. Response body: %s", http.StatusOK, userID, resp.StatusCode, string(body))
				return
			} else {
				t.Errorf("User %d Success to book coupon", userID)
				success++
			}

			var respBody map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&respBody)
			if err != nil {
				t.Errorf("Failed to decode response body for user %d: %v", userID, err)
				return
			}
			assert.Equal(t, "coupon booked successfully", respBody["message"])
		}(i)
	}

	wg.Wait()
	t.Logf("%d users got the coupon", success)
}
