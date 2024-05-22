package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/segmentio/kafka-go"
)

var (
	rdb            *redis.Client
	writer         *kafka.Writer
	appointmentSet map[int]struct{}
	bookSet        sync.Map // Use sync.Map for thread-safe operations
	canBook        bool
	startHour      int
	startMinute    int
)

func main() {
	// Use all available CPU cores
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Set Gin to release mode
	gin.SetMode(gin.ReleaseMode)

	// Parse command line arguments
	flag.IntVar(&startHour, "hour", 23, "hour to start booking")
	flag.IntVar(&startMinute, "minute", 0, "minute to start booking")
	flag.Parse()

	ctx := context.Background()
	defer ctx.Done()

	// Initialize Redis
	initRedis(ctx)

	// Initialize Kafka writer
	initKafkaWriter()
	defer writer.Close()

	appointmentSet = make(map[int]struct{})
	canBook = false

	r := gin.New()
	r.Use(gin.Recovery())

	r.POST("/appointment", handleAppointment)
	r.POST("/book_coupon", handleBookCoupon)

	go setAvailable(ctx)
	go setAppointment(ctx)
	go consumeKafkaMessages(ctx)

	r.Run(":8080")
}

func initRedis(ctx context.Context) {
	rdb = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   0,
	})

	if err := rdb.Del(ctx, "appointments").Err(); err != nil {
		log.Fatalf("failed to delete 'appointments' key: %v", err)
	}
	if err := rdb.Del(ctx, "available").Err(); err != nil {
		log.Fatalf("failed to delete 'available' key: %v", err)
	}
}

func initKafkaWriter() {
	writer = kafka.NewWriter(kafka.WriterConfig{
		Brokers:  []string{"localhost:9092"},
		Topic:    "user_ids",
		Balancer: &kafka.LeastBytes{},
	})
}

func handleAppointment(c *gin.Context) {
	now := time.Now()
	startTime := time.Date(now.Year(), now.Month(), now.Day(), startHour, startMinute, 0, 0, now.Location())

	// Check if the current time is within the 5 minutes before the start time
	if now.Before(startTime.Add(-5*time.Minute)) || now.After(startTime) {
		c.JSON(400, gin.H{"error": "appointments can only be made within 5 minutes before start time"})
		return
	}

	userID, err := getUserID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if err := rdb.SAdd(c, "appointments", fmt.Sprintf("%v", userID)).Err(); err != nil {
		c.JSON(500, gin.H{"error": "failed to schedule appointment"})
		return
	}

	c.JSON(200, gin.H{"message": "appointment scheduled successfully"})
}

func handleBookCoupon(c *gin.Context) {
	now := time.Now()
	startTime := time.Date(now.Year(), now.Month(), now.Day(), startHour, startMinute, 0, 0, now.Location())

	// Check if the current time is within the 1 minute after the start time
	if now.Before(startTime) || now.After(startTime.Add(1*time.Minute)) {
		c.JSON(400, gin.H{"error": "coupons can only be booked within 1 minute after start time"})
		return
	}

	if !canBook {
		c.JSON(500, gin.H{"error": "coupon booking time has not started yet"})
		return
	}

	userID, err := getUserID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	if !isUserEligibleForCoupon(userID) {
		c.JSON(500, gin.H{"error": "user is not eligible for coupon"})
		return
	}

	if err := bookCouponForUser(c, userID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "coupon booked successfully"})
}

// Helper function to get userID from request body
func getUserID(c *gin.Context) (int, error) {
	var reqBody struct {
		UserID int `json:"userID"`
	}
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		return 0, fmt.Errorf("invalid request body")
	}

	if reqBody.UserID == 0 {
		return 0, fmt.Errorf("user ID is not present in the request body")
	}

	return reqBody.UserID, nil
}

func isUserEligibleForCoupon(userID int) bool {
	_, existsInAppointments := appointmentSet[userID]
	_, existsInBookSet := bookSet.Load(userID)
	return existsInAppointments && !existsInBookSet
}

func bookCouponForUser(ctx context.Context, userID int) error {
	newValue, err := rdb.Decr(ctx, "available").Result()
	if err != nil {
		if err == redis.Nil {
			return fmt.Errorf("coupon booking time has not started yet")
		}
		return fmt.Errorf("failed to book coupon: %v", err)
	}
	if newValue < 0 {
		return fmt.Errorf("coupon issuance has ended")
	}

	if err := writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(fmt.Sprintf("%v", userID)),
		Value: []byte(fmt.Sprintf("%v", userID)),
	}); err != nil {
		return fmt.Errorf("failed to write message to Kafka: %v", err)
	}

	bookSet.Store(userID, struct{}{})
	return nil
}

func setAppointment(ctx context.Context) {
	for {
		now := time.Now()
		if now.Hour() >= startHour && now.Minute() >= startMinute {
			log.Println("start to setAppointment")

			userIDs, err := rdb.SMembers(ctx, "appointments").Result()
			if err != nil {
				log.Println("failed to get user IDs from appointments set:", err)
				continue
			}

			for _, s := range userIDs {
				userID, err := strconv.Atoi(s)
				if err != nil {
					log.Println("failed to convert userID to int:", err)
					continue
				}
				appointmentSet[userID] = struct{}{}
			}

			canBook = true
			return
		}
		time.Sleep(time.Second)
	}
}

func setAvailable(ctx context.Context) {
	for {
		now := time.Now()
		if now.Hour() >= startHour && now.Minute() >= startMinute {
			log.Println("start to setAvailable")

			userIDs, err := rdb.SMembers(ctx, "appointments").Result()
			if err != nil {
				log.Println("failed to get user IDs from appointments set:", err)
				continue
			}

			size := len(userIDs)
			available := size / 5
			log.Println("setAvailable:", available)

			if err := rdb.Set(ctx, "available", available, 0).Err(); err != nil {
				log.Println("failed to save set size:", err)
			} else {
				log.Println("appointments size saved to Redis:", size)
				return
			}
		}
		time.Sleep(time.Second)
	}
}

func consumeKafkaMessages(ctx context.Context) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  []string{"localhost:9092"},
		Topic:    "user_ids",
		GroupID:  "group_id",
		MinBytes: 10e3,
		MaxBytes: 10e6,
	})
	defer reader.Close()

	count := 0

	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			log.Println("error reading message:", err)
			continue
		}

		userID := string(msg.Key)
		if exists, err := rdb.SIsMember(ctx, "appointments", userID).Result(); err == nil && exists {
			// TODO:
		}

		if err := reader.CommitMessages(ctx, msg); err != nil {
			// log.Println("failed to commit message:", err)
		} else {
			count++
			// log.Printf("received message: key=%s, value=%s\n", userID, string(msg.Value))
		}
		if count%1000 == 0 {
			log.Printf("kafka already consumed %d message", count)
		}
	}
}
