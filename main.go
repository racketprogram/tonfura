package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/segmentio/kafka-go"
)

var (
	rdb    *redis.Client
	filter *bloom.BloomFilter
	writer *kafka.Writer
)

func main() {
	// Connect to Redis
	rdb = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // Redis server address
		Password: "",               // Redis server password (if any)
		DB:       0,                // Use default database
	})

	filter = bloom.NewWithEstimates(1000000, 0.01)

	// Kafka configuration
	config := kafka.WriterConfig{
		Brokers:  []string{"localhost:9092"}, // Kafka server address
		Topic:    "user_ids",                 // Kafka topic name
		Balancer: &kafka.LeastBytes{},        // Load balancer
	}
	// Create a new Kafka writer
	writer = kafka.NewWriter(config)
	defer writer.Close()

	r := gin.Default()

	// Endpoint for making an appointment
	r.POST("/appointment", func(c *gin.Context) {
		// TODO: check time

		userID, exists := c.Get("userID")
		if !exists {
			c.JSON(400, gin.H{"error": "User ID not found in JWT"})
			return
		}

		// Add user to the set of appointments
		err := rdb.SAdd(c, "appointments", fmt.Sprintf("%v", userID)).Err()
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to schedule appointment"})
			return
		}

		c.JSON(200, gin.H{"message": "Appointment scheduled successfully"})
	})

	// Endpoint for booking a coupon
	r.POST("/book_coupon", func(c *gin.Context) {
		// TODO: check time

		userID, exists := c.Get("userID")
		if !exists {
			c.JSON(400, gin.H{"error": "User ID not found in JWT"})
			return
		}

		// Check if the user is in the Bloom filter (already made an appointment)
		// Even if false positive, it can be reconfirmed later
		if !filter.Test([]byte(fmt.Sprintf("%v", userID))) {
			c.JSON(500, gin.H{"error": "User has not made an appointment yet"})
			return
		}

		// Decrement available coupon count
		newValue, err := rdb.Decr(c, "available").Result()
		if err != nil {
			if err == redis.Nil {
				c.JSON(500, gin.H{"error": "Coupon booking time has not started yet"})
				return
			}
			c.JSON(500, gin.H{"error": "Failed to book coupon"})
			return
		}
		if newValue < 0 {
			c.JSON(500, gin.H{"message": "Coupon issuance has ended"})
			return
		}

		err = writer.WriteMessages(context.TODO(),
			kafka.Message{
				Key:   []byte(fmt.Sprintf("%v", userID)), // User ID as message key
				Value: []byte(fmt.Sprintf("%v", userID)), // User ID as message value
			},
		)
		if err != nil {
			fmt.Println("Failed to write message to Kafka:", err)
			c.JSON(500, gin.H{"error": "Failed to book coupon"})
			return
		}

		c.JSON(200, gin.H{"message": "success book"})
	})

	go calculateSetSize(true)
	go consume()

	r.Run(":8080")
}

func calculateSetSize(isAdmin bool) {
	for {
		// Get current time
		now := time.Now()

		// If it's 23:00, calculate the set size
		if now.Hour() == 23 && now.Minute() == 0 {
			// Get all user IDs from the appointments set
			userIDs, err := rdb.SMembers(context.TODO(), "appointments").Result()
			if err != nil {
				fmt.Println("Failed to get user IDs from appointments set:", err)
				return
			}

			// Add all user IDs to the Bloom filter
			for _, userID := range userIDs {
				filter.Add([]byte(userID))
			}

			size := len(userIDs)

			fmt.Println("calculateSetSize:", size)

			if isAdmin {

				// Convert size to string
				available := int(int(size) / 5)

				// Save size to Redis
				err = rdb.Set(context.TODO(), "available", available, 0).Err()
				if err != nil {
					fmt.Println("Failed to save set size:", err)
				} else {
					fmt.Println("Appointments size saved to Redis:", size)
					return
				}
			} else {
				return
			}
		}

		// Sleep for 1 minute
		time.Sleep(time.Minute)
	}
}

func consume() {
	// Kafka configuration
	config := kafka.ReaderConfig{
		Brokers:  []string{"localhost:9092"}, // Kafka 服務器地址
		Topic:    "user_ids",                 // Kafka 主題名稱
		GroupID:  "group_id",                 // 消費者組 ID
		MinBytes: 10e3,                       // 最小字節數
		MaxBytes: 10e6,                       // 最大字節數
	}
	// Create a new Kafka reader
	reader := kafka.NewReader(config)
	defer reader.Close()

	ctx := context.Background()

	// Start consuming messages
	for {
		// Read a message from Kafka topic
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			log.Println("Error reading message:", err)
			continue
		}

		userID := string(msg.Key)

		exists, err := rdb.SIsMember(ctx, "appointments", userID).Result()
		if err != nil {
			continue
		}

		if exists {
			// Add user to the set of appointments
			err := rdb.SAdd(ctx, "appointments", fmt.Sprintf("%v", userID)).Err()
			if err != nil {
				continue
			}
		}

		// TODO: record it to the DB.

		reader.CommitMessages(ctx, msg)

		// Print the received message
		fmt.Printf("Received message: key=%s, value=%s\n", string(msg.Key), string(msg.Value))
	}
}
