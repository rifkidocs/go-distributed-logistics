package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
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
	log.Println("Starting Warehouse & Logistics Load Simulator...")

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

	// 3. Simulate High Concurrency Order placement
	// MacBook Pro M3 ID has 100 units in stock.
	// We make 10 concurrent requests reserving 15 units each (10 * 15 = 150 units requested).
	// Only 6 should succeed (6 * 15 = 90 units reserved, remaining 10). The rest should fail.
	macbookID := "11111111-1111-1111-1111-111111111111"
	concurrency := 10
	unitsPerOrder := 15

	log.Printf("Starting concurrency test: Placing %d orders of MacBook Pro (15 units each) concurrently...", concurrency)

	var wg sync.WaitGroup
	results := make([]string, concurrency)
	client := &http.Client{}

	start := time.Now()
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			orderReq := OrderReq{
				ItemID:      macbookID,
				Quantity:    unitsPerOrder,
				Destination: fmt.Sprintf("Jakarta Gateway Hub Zone %d", index+1),
			}
			orderBytes, _ := json.Marshal(orderReq)

			req, _ := http.NewRequest("POST", baseURL+"/order", bytes.NewBuffer(orderBytes))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+loginRes.AccessToken)

			res, err := client.Do(req)
			if err != nil {
				results[index] = fmt.Sprintf("Request %d: Failed with error: %v", index+1, err)
				return
			}
			defer res.Body.Close()

			resBytes, _ := io.ReadAll(res.Body)
			var orderRes OrderRes
			_ = json.Unmarshal(resBytes, &orderRes)

			results[index] = fmt.Sprintf("Request %d (Status %d): Success=%t | OrderID=%s | Message=%s",
				index+1, res.StatusCode, orderRes.Success, orderRes.OrderID, orderRes.Message)
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	log.Println("\n--- Concurrency Test Results ---")
	successCount := 0
	for _, res := range results {
		fmt.Println(res)
		if bytes.Contains([]byte(res), []byte("Success=true")) {
			successCount++
		}
	}
	log.Printf("\nConcurrency test completed in %v", duration)
	log.Printf("Total Successful Reservations: %d/%d (Expect exactly 6)", successCount, concurrency)
	log.Println("Verify your Jaeger Dashboard (http://localhost:16686) to trace transaction flows!")
}
