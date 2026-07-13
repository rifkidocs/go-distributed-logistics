package main

import (
	"context"
	"database/sql"
	"log"
	"net"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"google.golang.org/grpc"

	pb "github.com/rifkidocs/go-distributed-logistics/api/inventory"
	"github.com/rifkidocs/go-distributed-logistics/internal/config"
	"github.com/rifkidocs/go-distributed-logistics/internal/database/inventory"
	"github.com/rifkidocs/go-distributed-logistics/internal/event"
	"github.com/rifkidocs/go-distributed-logistics/internal/services"
	"github.com/rifkidocs/go-distributed-logistics/internal/telemetry"
)

func main() {
	cfg := config.LoadConfig()

	log.Println("Starting Inventory Service...")

	// OpenTelemetry init
	ctx := context.Background()
	tp, shutdown, err := telemetry.InitTracer(ctx, "inventory-service", cfg.JaegerURL)
	if err != nil {
		log.Printf("Warning: failed to init telemetry: %v", err)
	} else {
		defer shutdown(ctx)
		_ = tp
	}

	// Database init
	db, err := sql.Open("postgres", cfg.InventoryDBURL)
	if err != nil {
		log.Fatalf("failed to connect to DB: %v", err)
	}
	defer db.Close()

	// Redis init
	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.RedisURL,
	})
	defer rdb.Close()

	// Redsync distributed lock init
	pool := goredis.NewPool(rdb)
	rs := redsync.New(pool)

	// NATS JetStream init
	jsMgr, natsConn, err := event.NewJetStreamManager(cfg.NatsURL)
	if err != nil {
		log.Fatalf("failed to connect to NATS: %v", err)
	}
	defer natsConn.Close()

	// Seed database data for testing
	seedDatabase(db)

	// Start gRPC Server
	lis, err := net.Listen("tcp", ":"+cfg.InventoryPort)
	if err != nil {
		log.Fatalf("failed to listen on port %s: %v", cfg.InventoryPort, err)
	}

	s := grpc.NewServer()
	inventorySvc := services.NewInventoryService(db, rdb, rs, jsMgr)
	pb.RegisterInventoryServiceServer(s, inventorySvc)

	log.Printf("Inventory Service listening on gRPC port %s", cfg.InventoryPort)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve gRPC: %v", err)
	}
}

func seedDatabase(db *sql.DB) {
	queries := inventorydb.New(db)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Println("Seeding inventory data...")

	// Create test items
	testItem1 := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	_, _ = queries.CreateItem(ctx, inventorydb.CreateItemParams{
		ID:          testItem1,
		Name:        "MacBook Pro M3",
		Sku:         "MACBOOK-M3-001",
		Description: sql.NullString{String: "Apple MacBook Pro M3 Chip 16GB RAM 512GB SSD", Valid: true},
		Price:       "24999000.00", // Rp 24.999.000
	})

	testItem2 := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	_, _ = queries.CreateItem(ctx, inventorydb.CreateItemParams{
		ID:          testItem2,
		Name:        "iPhone 15 Pro",
		Sku:         "IPHONE-15P-002",
		Description: sql.NullString{String: "Apple iPhone 15 Pro 128GB", Valid: true},
		Price:       "18500000.00", // Rp 18.500.000
	})

	// Seed Stock Level for Warehouse 1
	warehouseID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	
	// MacBook Stock
	_, _ = queries.SetStock(ctx, inventorydb.SetStockParams{
		WarehouseID: warehouseID,
		ItemID:      testItem1,
		Quantity:    100, // Starting stock
	})

	// iPhone Stock
	_, _ = queries.SetStock(ctx, inventorydb.SetStockParams{
		WarehouseID: warehouseID,
		ItemID:      testItem2,
		Quantity:    50, // Starting stock
	})

	log.Println("Seeding complete.")
}
