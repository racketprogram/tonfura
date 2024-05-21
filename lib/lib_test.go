package lib

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/go-redis/redis/v8"
)

var ctx = context.Background()

func TestRedisReadWrite(t *testing.T) {
	// 創建一個 Redis 連接
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379", // Redis 服務器地址
		DB:   0,                // 使用默認的數據庫
	})

	// 清空 Redis 中的所有數據
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

	// 測試讀取 Redis
	val, err := rdb.Get(ctx, "available").Int()
	if err != nil {
		t.Errorf("Failed to get value from Redis: %v", err)
	}

	fmt.Println(val)

	// 驗證讀取的值是否正確
	if val != available {
		t.Errorf("Expected value 'test_value', got '%d'", val)
	}

	// 設置並發數量
	numThreads := 10
	// 設置每個 Goroutine 的 DECR 數量
	decrementAmount := 1
	// 設置預期結果
	expectedValue := val - (numThreads * decrementAmount)

	// 創建一個 WaitGroup，用於等待所有 Goroutine 完成
	var wg sync.WaitGroup
	wg.Add(numThreads)

	// 启动 numThreads 个 Goroutine 进行并发 DECR 操作
	for i := 0; i < numThreads; i++ {
		go func() {
			defer wg.Done()
			// 每个 Goroutine 执行 DECR 操作
			// 執行 DECRBY 操作並獲取結果
			newValue, err := rdb.DecrBy(ctx, "available", int64(decrementAmount)).Result()
			if err != nil {
				t.Errorf("Failed to decrement value: %v", err)
				return
			}
			_ = newValue
			// fmt.Println(newValue)
		}()
	}

	// 等待所有 Goroutine 完成
	wg.Wait()

	// 檢查最終的值是否與預期相同
	finalValue, err := rdb.Get(ctx, "available").Int()
	if err != nil {
		t.Errorf("Failed to get final value from Redis: %v", err)
	}

	if finalValue != expectedValue {
		t.Errorf("Expected final value %d, got %d", expectedValue, finalValue)
	}

	fmt.Println(finalValue)
}
