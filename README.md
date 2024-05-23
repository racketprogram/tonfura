# 搶購優惠券系統

## 項目簡介
該項目設計了一套優惠券搶購機制，每天指定一個時間開放索取優惠券。用戶需要提前1-5分鐘進行預約，優惠券的數量為預約用戶數量的20%。在搶購時，每個用戶只能搶一次，搶購時間只有1分鐘。

## 設計說明
假設每個用戶都有唯一的 `user_id`，在登記期間會將此 ID 記錄在 Redis 集合中。登記時間結束後，領導進程（Leader Process）會透過 Redis 集合統計登記的數量並計算出優惠券的數量（20%），然後將結果存到 Redis 的鍵值對中。此時，從屬進程（Follower Process）也會把所有登記者的 `user_id` 從 Redis 集合取出並存到本地內存。

開始搶票後，API 會檢查用戶的 `user_id` 是否有提前登記過或是已經搶過，若任一項符合則拒絕請求。符合資格則會透過 Redis 原子操作去扣除 Redis 上優惠券的數量，若數量不足則回覆搶票失敗。若數量還夠則會將此請求寫入 Kafka 中並回覆用戶搶票成功。由於我們預設 Kafka 開啟了複製集（Replication Set）功能，所以只要有回覆給用戶搶票成功後，那就是最終一致性的成功，不會有任何失敗的狀況。寫入 Kafka 後再讀出來的操作就沒什麼難度了，只要保持冪等性操作，就可以重複消費同一則訊息來保證優惠券一定交到用戶手中。

#### 補充事項
* Leader process 只會做一次，避免優惠券數量又被捕滿了。
* Follower process 如果因為什麼原因掛了也只要重啟就好，搶票邏輯不會有影響。
* 一個額外情況就是 redis 數量扣除了但寫入 kafka 失敗，那這張票就在本次搶票中沒辦法順利交付出去，需要後續清票邏輯。
* 同一個用戶若是多次寫入了 kafka 也沒有關係，因為後續操作設計為冪等操作，所以還是只有搶到一張。

### 設計理念
讓每個用戶是真的有參與搶票，中途沒有任何欺騙或是不公平的狀況發生。

### 設計瓶頸
在我本地的瓶頸就是瞬間打開大量的 tcp 連線會有奇怪的錯誤產生(可能是 windows 的原因，參考 errorinfo)，在生產環境只要配置合理的 load balancer 搭配一定數量的 follower process 就不會有這個瓶頸。對於 redis 及 kafka 的操作也編寫了獨立的測試來保證在 30000 人同時使用下的可用性，也確認這部分不會是瓶頸。

## 目錄結構
- `main.go`：項目的主入口文件。
- `integration_test.go`：集成測試文件。
- `Makefile`：項目構建和管理工具的配置文件。
- `kafka_concurrency_test.go`：Kafka 並發測試文件。
- `redis_concurrency_test.go`：Redis 並發測試文件。
- `redis_test.go`：Redis 測試文件。
- `docker-compose.yml`：Docker Compose 配置文件。

## 運行

### 基礎設施
```sh
docker-compose up -d
```
#### 說明
其中主要有 redis 和 kafka 用來作為搶票邏輯的中心化組件

### 主程式
```sh
go build .\main.go
.\main.exe -hour=23 -minute=0 -lead=true -consumer=true
```

#### 命令參數說明
- `-hour=23`：設定發放優惠券的小時（此處為23點）。
- `-minute=0`：設定發放優惠券的分鐘（此處為0分）。
- `-lead=true`：是否為 leader process 來處理優惠券數量。
- `-consumer=true`：是否啟用消費者功能來處理搶購。

### 集成測試(模擬用戶行為)
windows
```sh
$env:START_HOUR="23"; $env:START_MINUTE="0"; go test ./integration_test/... > log
```
linux/mac
```sh
export START_HOUR="23"
export START_MINUTE="0"
go test ./integration_test/... > log 
```

#### 命令參數說明
- `export START_HOUR="23"`：讓用戶得知發放優惠券的小時。
- `export START_MINUTE="0"`：讓用戶得知發放發放優惠券的分鐘。

## 文件說明

### `main.go`
項目的主入口，包含了服務的啟動和基礎路由配置。

### `integration_test.go`
集成測試文件，包含了對整個系統的集成測試，確保各個模塊協同工作。

### `Makefile`
用於構建和管理項目的 Makefile。提供了構建、運行、測試等常用命令。

### `kafka_concurrency_test.go`
Kafka 並發測試文件，用於測試在高並發環境下，Kafka 消息處理的性能和可靠性。

### `redis_concurrency_test.go`
Redis 並發測試文件，用於測試在高並發環境下，Redis 數據存取的性能和可靠性。

### `redis_test.go`
Redis 測試文件，包含了對 Redis 操作的基本測試。

### `docker-compose.yml`
Docker Compose 配置文件，用於快速啟動項目所需的各種服務，如數據庫、緩存等。
