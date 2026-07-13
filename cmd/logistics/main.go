package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net"
	"time"

	_ "github.com/lib/pq"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc"

	pb "github.com/rifkidocs/go-distributed-logistics/api/logistics"
	"github.com/rifkidocs/go-distributed-logistics/internal/config"
	"github.com/rifkidocs/go-distributed-logistics/internal/event"
	"github.com/rifkidocs/go-distributed-logistics/internal/services"
	"github.com/rifkidocs/go-distributed-logistics/internal/telemetry"
)

func main() {
	cfg := config.LoadConfig()

	log.Println("Starting Logistics Service...")

	// OpenTelemetry init
	ctx := context.Background()
	tp, shutdown, err := telemetry.InitTracer(ctx, "logistics-service", cfg.JaegerURL)
	if err != nil {
		log.Printf("Warning: failed to init telemetry: %v", err)
	} else {
		defer shutdown(ctx)
		_ = tp
	}

	// Database init
	db, err := sql.Open("postgres", cfg.LogisticsDBURL)
	if err != nil {
		log.Fatalf("failed to connect to DB: %v", err)
	}
	defer db.Close()

	// NATS JetStream init
	jsMgr, natsConn, err := event.NewJetStreamManager(cfg.NatsURL)
	if err != nil {
		log.Fatalf("failed to connect to NATS: %v", err)
	}
	defer natsConn.Close()

	logisticsSvc := services.NewLogisticsService(db)

	// Subscribe to NATS stock.reserved events using JetStream durable subscription
	go startNatsEventListener(jsMgr, db, logisticsSvc)

	// Start gRPC Server
	lis, err := net.Listen("tcp", ":"+cfg.LogisticsPort)
	if err != nil {
		log.Fatalf("failed to listen on port %s: %v", cfg.LogisticsPort, err)
	}

	s := grpc.NewServer()
	pb.RegisterLogisticsServiceServer(s, logisticsSvc)

	log.Printf("Logistics Service listening on gRPC port %s", cfg.LogisticsPort)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve gRPC: %v", err)
	}
}

type StockReservedPayload struct {
	OrderID     string `json:"order_id"`
	ItemID      string `json:"item_id"`
	Quantity    int32  `json:"quantity"`
	Destination string `json:"destination"`
}

func startNatsEventListener(jsMgr *event.JetStreamManager, db *sql.DB, svc *services.LogisticsService) {
	subject := "stock.reserved"
	queueName := "logistics-worker"

	log.Printf("Subscribing to NATS subject: %s, queue: %s", subject, queueName)

	_, err := jsMgr.NC.QueueSubscribe(subject, queueName, func(msg *nats.Msg) {
		// Extract Tracing Context from NATS header
		ctx := event.ExtractTraceContext(context.Background(), msg)

		tr := otel.Tracer("logistics-service")
		ctx, span := tr.Start(ctx, "NATS Event: stock.reserved")
		defer span.End()

		log.Printf("Received stock.reserved event!")

		var payload StockReservedPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			log.Printf("Failed to unmarshal event data: %v", err)
			msg.Nak()
			return
		}

		log.Printf("Processing logistics assignment for Order %s, Item %s, Qty %d", payload.OrderID, payload.ItemID, payload.Quantity)

		// Call service to create shipment
		_, err := svc.CreateShipment(ctx, &pb.CreateShipmentRequest{
			OrderId:     payload.OrderID,
			ItemId:      payload.ItemID,
			Quantity:    payload.Quantity,
			Destination: payload.Destination,
		})
		if err != nil {
			log.Printf("Failed to create shipment for order %s: %v", payload.OrderID, err)
			msg.NakWithDelay(3 * time.Second) // Retry with delay
			return
		}

		log.Printf("Shipment created successfully for Order %s", payload.OrderID)
		msg.Ack()
	}, nats.ManualAck())

	if err != nil {
		log.Fatalf("NATS JetStream subscription failed: %v", err)
	}
}
