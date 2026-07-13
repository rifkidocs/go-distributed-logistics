package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const (
	baseURL = "http://localhost:8080"
)

type RegisterReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type LoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginRes struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type OrderReq struct {
	ItemID      string `json:"item_id"`
	Quantity    int    `json:"quantity"`
	Destination string `json:"destination"`
}

type OrderRes struct {
	OrderID string `json:"order_id"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func main() {
	// Flags for customization
	totalRequests := flag.Int("r", 100, "Total number of order requests to send")
	concurrency := flag.Int("c", 10, "Number of concurrent workers")
	flag.Parse()

	log.Println("Starting Warehouse & Logistics Load Simulator...")
	log.Printf("Configuration: Total Requests = %d, Concurrency = %d\n", *totalRequests, *concurrency)

	// 1. Register a test user
	username := fmt.Sprintf("sim_user_%d", time.Now().Unix())
	password := "securepassword123"

	log.Printf("Registering test user: %s", username)
	regBody, _ := json.Marshal(RegisterReq{
		Username: username,
		Password: password,
		Role:     "staff",
	})
	resp, err := http.Post(baseURL+"/register", "application/json", bytes.NewBuffer(regBody))
	if err != nil {
		log.Fatalf("failed to register user: %v", err)
	}
	resp.Body.Close()

	// 2. Login to get JWT Token
	log.Println("Logging in...")
	loginBody, _ := json.Marshal(LoginReq{
		Username: username,
		Password: password,
	})
	resp, err = http.Post(baseURL+"/login", "application/json", bytes.NewBuffer(loginBody))
	if err != nil {
		log.Fatalf("failed to login: %v", err)
	}
	defer resp.Body.Close()

	var loginRes LoginRes
	bodyBytes, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(bodyBytes, &loginRes)

	if loginRes.AccessToken == "" {
		log.Fatalf("failed to retrieve JWT access token: %s", string(bodyBytes))
	}
	log.Println("JWT Authentication Successful!")

	// 3. Simulate Concurrency Order placement
	macbookID := "11111111-1111-1111-1111-111111111111"
	
	var wg sync.WaitGroup
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Channel to distribute work
	jobs := make(chan int, *totalRequests)
	for i := 0; i < *totalRequests; i++ {
		jobs <- i
	}
	close(jobs)

	var successCount int64
	var failureCount int64
	var totalDurationMs int64

	start := time.Now()

	// Spawn workers
	for w := 0; w < *concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for range jobs {
				orderReq := OrderReq{
					ItemID:   macbookID,
					Quantity: 1, // order 1 unit to avoid instant stock drain
					Destination: fmt.Sprintf("Jakarta Gateway Hub Worker %d", workerID),
				}
				orderBytes, _ := json.Marshal(orderReq)

				reqStart := time.Now()
				req, err := http.NewRequest("POST", baseURL+"/order", bytes.NewBuffer(orderBytes))
				if err != nil {
					atomic.AddInt64(&failureCount, 1)
					continue
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+loginRes.AccessToken)

				res, err := client.Do(req)
				if err != nil {
					atomic.AddInt64(&failureCount, 1)
					continue
				}
				
				resBytes, _ := io.ReadAll(res.Body)
				res.Body.Close()

				duration := time.Since(reqStart).Milliseconds()
				atomic.AddInt64(&totalDurationMs, duration)

				var orderRes OrderRes
				_ = json.Unmarshal(resBytes, &orderRes)

				if res.StatusCode == http.StatusOK && orderRes.Success {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&failureCount, 1)
				}
			}
		}(w)
	}

	wg.Wait()
	totalTime := time.Since(start)

	// Output Results
	log.Println("\n--- Stress Test Results ---")
	log.Printf("Total Time Elapsed  : %v\n", totalTime)
	log.Printf("Total Requests Done : %d\n", *totalRequests)
	log.Printf("Successful Orders   : %d\n", atomic.LoadInt64(&successCount))
	log.Printf("Failed/OutOfStock   : %d\n", atomic.LoadInt64(&failureCount))

	avgLatency := float64(atomic.LoadInt64(&totalDurationMs)) / float64(*totalRequests)
	log.Printf("Average Latency     : %.2f ms\n", avgLatency)
	log.Printf("Requests / Second   : %.2f RPS\n", float64(*totalRequests)/totalTime.Seconds())
	log.Println("\nTip: Check Jaeger Dashboard (http://localhost:16686) for detailed request flows!")
}
