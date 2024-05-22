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
	bookSet        sync.Map  // Use sync.Map for thread-safe operations
	canBook        bool      // 搶票開始前的準備資料必須完成後才透過此參數讓 api 開放搶票
	startTime      time.Time // 搶票開始時間
	startHour      int
	startMinute    int
	isLead         bool
	isConsumer     bool
)

func main() {
	// Use all available CPU cores
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Set Gin to release mode
	gin.SetMode(gin.ReleaseMode)

	// Parse command line arguments
	flag.IntVar(&startHour, "hour", 23, "hour to start booking")
	flag.IntVar(&startMinute, "minute", 0, "minute to start booking")
	flag.BoolVar(&isLead, "lead", false, "is lead")
	flag.BoolVar(&isConsumer, "consumer", false, "is consumer")
	flag.Parse()

	now := time.Now()
	startTime = time.Date(now.Year(), now.Month(), now.Day(), startHour, startMinute, 0, 0, now.Location())

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

	// 搶票時間到時從 redis 中取出所有登記在 redis 的 user id 並存到本地以利查詢。
	go setAppointment(ctx)

	if isLead {
		// 被指定為 leader 的 process 在搶票時間到時從 redis 中取出所有有登記的 user id 數量並存到 redis 以利查詢。
		// 也會在此去設定優惠券的數量。
		go setAvailable(ctx)
	}

	if isConsumer {
		// 為了展示可以正常消費訊息而寫的邏輯
		go consumeKafkaMessages(ctx)
	}

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

// 讓用戶來預約搶優惠券的資格
func handleAppointment(c *gin.Context) {
	now := time.Now()

	// Check if the current time is within the 1-5 minutes before the start time
	if now.Before(startTime.Add(-5*time.Minute)) || now.After(startTime.Add(-time.Minute)) {
		c.JSON(400, gin.H{"error": "appointments can only be made within 5 minutes before start time"})
		return
	}

	// 此處為了方便省略了從 jwt 拿取 user id 的程式
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 把 user id 寫到 redis 中的 set。
	if err := rdb.SAdd(c, "appointments", fmt.Sprintf("%v", userID)).Err(); err != nil {
		c.JSON(500, gin.H{"error": "failed to schedule appointment"})
		return
	}

	c.JSON(200, gin.H{"message": "appointment scheduled successfully"})
}

func handleBookCoupon(c *gin.Context) {
	now := time.Now()

	// Check if the current time is within the 1 minute after the start time
	if now.Before(startTime) || now.After(startTime.Add(1*time.Minute)) {
		c.JSON(400, gin.H{"error": "coupons can only be booked within 1 minute after start time"})
		return
	}

	if !canBook {
		c.JSON(500, gin.H{"error": "coupon booking time has not started yet"})
		return
	}

	// 此處為了方便省略了從 jwt 拿取 user id 的程式
	userID, err := getUserID(c)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	// 用來確認用戶是否有資格搶優惠券
	if !isUserEligibleForCoupon(userID) {
		c.JSON(500, gin.H{"error": "user is not eligible for coupon"})
		return
	}

	if err := bookCouponForUser(c, userID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// 只要使用的 kafka 有開啟 replication
	// 從此函數返回給用戶時便可對用戶保證他們已經搶到優惠券了
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
	// 這是一個 local memory 檢查用戶是否搶到過優惠券
	// 這邊就算因為用戶請求走到了不同的 proceess 導致沒檢查到也沒關係，因為後續 kafka 消費者的操作會保證冪等。
	_, existsInBookSet := bookSet.Load(userID)
	return existsInAppointments && !existsInBookSet
}

func bookCouponForUser(ctx context.Context, userID int) error {
	// 從 redis 執行原子化減一的操作並拿回優惠券數量。
	// 保證了只發放指定數量優惠券，只會少發不會超發。
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

	// 寫入 kafka
	if err := writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(fmt.Sprintf("%v", userID)),
		Value: []byte(fmt.Sprintf("%v", userID)),
	}); err != nil {
		return fmt.Errorf("failed to write message to Kafka: %v", err)
	}

	// 動態修改此 set 以標示該用戶已經搶到過了。
	bookSet.Store(userID, struct{}{})
	return nil
}

// 在搶票時間開始前三十秒準備資料
func timeToPrepare() bool {
	now := time.Now()
	return now.After(startTime.Add(-30 * time.Second))
}

func setAppointment(ctx context.Context) {
	for {
		if timeToPrepare() {
			log.Println("start to setAppointment")

			// 取出 redis set 中所有的值
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

		select {
		case <-ctx.Done():
			log.Println("context cancelled, exiting setAppointment")
			return
		case <-time.After(time.Second):
			// Continue the loop after sleeping for one second
		}
	}
}

func setAvailable(ctx context.Context) {
	for {
		if timeToPrepare() {
			log.Println("start to setAvailable")

			userIDs, err := rdb.SMembers(ctx, "appointments").Result()
			if err != nil {
				log.Println("failed to get user IDs from appointments set:", err)
				continue
			}

			// 根據題目以我的設計我認為要公平競爭，那優惠券數量就是設定登記者數量的 20% 就好。
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

		select {
		case <-ctx.Done():
			log.Println("context cancelled, exiting setAvailable")
			return
		case <-time.After(time.Second):
			// Continue the loop after sleeping for one second
		}
	}
}

// 用來驗證搶到票的用戶有被寫入 kafka
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
			// TODO: 這邊可以做比較慢的操作，比如寫入資料到關聯式資料庫。寫入時還可以檢查該用戶是否已經有優惠券了。
			// NOTE: 這邊的任何操作都必須是冪等操作，也就是可以重複執行但其實只有執行一次。

			// 關聯資料庫範例
			// table column - id user_id coupon_id
			// index - unique composite key (userd_id + coupon_id)
		}

		// 無論上面的邏輯做了多少事情，都必須完全做完後才會 commit message，為的是如果沒有完全做完的話可以 retry。
		if err := reader.CommitMessages(ctx, msg); err != nil {
			// log.Println("failed to commit message:", err)
		} else {
			count++
			// log.Printf("received message: key=%s, value=%s\n", userID, string(msg.Value))
		}
		if count%1000 == 0 {
			log.Printf("kafka already consumed %d message", count)
		}

		select {
		case <-ctx.Done():
			log.Println("context cancelled, exiting consumeKafkaMessages")
			return
		default:
		}
	}
}
