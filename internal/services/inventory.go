package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/go-redsync/redsync/v4"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/yourusername/project-backend-go/api/inventory"
	inventorydb "github.com/yourusername/project-backend-go/internal/database/inventory"
	"github.com/yourusername/project-backend-go/internal/event"
)

type InventoryService struct {
	pb.UnimplementedInventoryServiceServer
	db       *sql.DB
	queries  inventorydb.Querier
	redisCl  *redis.Client
	rsync    *redsync.Redsync
	jsMgr    *event.JetStreamManager
}

func NewInventoryService(db *sql.DB, redisCl *redis.Client, rsync *redsync.Redsync, jsMgr *event.JetStreamManager) *InventoryService {
	return &InventoryService{
		db:      db,
		queries: inventorydb.New(db),
		redisCl: redisCl,
		rsync:   rsync,
		jsMgr:   jsMgr,
	}
}

func (s *InventoryService) CheckStock(ctx context.Context, req *pb.CheckStockRequest) (*pb.CheckStockResponse, error) {
	tr := otel.Tracer("inventory-service")
	ctx, span := tr.Start(ctx, "CheckStock")
	defer span.End()

	itemID, err := uuid.Parse(req.ItemId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid item ID: %v", err)
	}

	// We use a fixed warehouse ID for local mock purposes
	warehouseID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	stock, err := s.queries.GetStock(ctx, inventorydb.GetStockParams{
		WarehouseID: warehouseID,
		ItemID:      itemID,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return &pb.CheckStockResponse{ItemId: req.ItemId, Quantity: 0}, nil
		}
		return nil, status.Errorf(codes.Internal, "failed to get stock: %v", err)
	}

	return &pb.CheckStockResponse{
		ItemId:   req.ItemId,
		Quantity: stock.Quantity,
	}, nil
}

func (s *InventoryService) ReserveStock(ctx context.Context, req *pb.ReserveStockRequest) (*pb.ReserveStockResponse, error) {
	tr := otel.Tracer("inventory-service")
	ctx, span := tr.Start(ctx, "ReserveStock")
	defer span.End()

	itemID, err := uuid.Parse(req.ItemId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid item ID: %v", err)
	}
	orderID, err := uuid.Parse(req.OrderId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid order ID: %v", err)
	}

	warehouseID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	// Acquire distributed lock for this item
	lockKey := fmt.Sprintf("lock:item:%s", req.ItemId)
	mutex := s.rsync.NewMutex(lockKey, redsync.WithExpiry(5*time.Second))
	if err := mutex.LockContext(ctx); err != nil {
		return nil, status.Errorf(codes.Aborted, "could not acquire lock for item: %v", err)
	}
	defer mutex.UnlockContext(ctx)

	// Execute DB Transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	txQueries := inventorydb.New(tx)

	// Get current stock
	stock, err := txQueries.GetStock(ctx, inventorydb.GetStockParams{
		WarehouseID: warehouseID,
		ItemID:      itemID,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return &pb.ReserveStockResponse{Success: false, Message: "item not found in stock"}, nil
		}
		return nil, status.Errorf(codes.Internal, "failed to query stock: %v", err)
	}

	if stock.Quantity < req.Quantity {
		return &pb.ReserveStockResponse{
			Success: false,
			Message: fmt.Sprintf("insufficient stock: available %d, requested %d", stock.Quantity, req.Quantity),
		}, nil
	}

	// Update stock level (set absolute quantity)
	newQty := stock.Quantity - req.Quantity
	_, err = txQueries.SetStock(ctx, inventorydb.SetStockParams{
		WarehouseID: warehouseID,
		ItemID:      itemID,
		Quantity:    newQty,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update stock: %v", err)
	}

	// Record to transaction ledger
	_, err = txQueries.CreateLedgerEntry(ctx, inventorydb.CreateLedgerEntryParams{
		ItemID:         uuid.NullUUID{UUID: itemID, Valid: true},
		WarehouseID:    warehouseID,
		QuantityChange: -req.Quantity,
		TransactionType: "RESERVE",
		OrderID:        uuid.NullUUID{UUID: orderID, Valid: true},
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create ledger entry: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to commit transaction: %v", err)
	}

	go func() {
		eventPayload := map[string]interface{}{
			"order_id":    req.OrderId,
			"item_id":     req.ItemId,
			"quantity":    req.Quantity,
			"destination": req.Destination,
		}
		data, _ := json.Marshal(eventPayload)
		msg := nats.NewMsg("stock.reserved")
		msg.Data = data
		msg.Header.Set("Content-Type", "application/json")
		event.InjectTraceContext(ctx, msg)

		if _, err := s.jsMgr.NC.PublishMsg(msg); err != nil {
			log.Printf("Failed to publish NATS stock.reserved event: %v", err)
		} else {
			log.Printf("Successfully published stock.reserved event for Order ID %s", req.OrderId)
		}
	}()

	return &pb.ReserveStockResponse{
		Success: true,
		Message: "stock reserved successfully",
	}, nil
}

func (s *InventoryService) ReleaseStock(ctx context.Context, req *pb.ReleaseStockRequest) (*pb.ReleaseStockResponse, error) {
	tr := otel.Tracer("inventory-service")
	ctx, span := tr.Start(ctx, "ReleaseStock")
	defer span.End()

	itemID, err := uuid.Parse(req.ItemId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid item ID: %v", err)
	}
	orderID, err := uuid.Parse(req.OrderId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid order ID: %v", err)
	}

	warehouseID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	// Acquire lock
	lockKey := fmt.Sprintf("lock:item:%s", req.ItemId)
	mutex := s.rsync.NewMutex(lockKey, redsync.WithExpiry(5*time.Second))
	if err := mutex.LockContext(ctx); err != nil {
		return nil, status.Errorf(codes.Aborted, "could not acquire lock for item: %v", err)
	}
	defer mutex.UnlockContext(ctx)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	txQueries := inventorydb.New(tx)

	// Add stock back
	_, err = txQueries.UpsertStock(ctx, inventorydb.UpsertStockParams{
		WarehouseID: warehouseID,
		ItemID:      itemID,
		Quantity:    req.Quantity,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to return stock: %v", err)
	}

	// Ledger entry
	_, err = txQueries.CreateLedgerEntry(ctx, inventorydb.CreateLedgerEntryParams{
		ItemID:         uuid.NullUUID{UUID: itemID, Valid: true},
		WarehouseID:    warehouseID,
		QuantityChange: req.Quantity,
		TransactionType: "RELEASE",
		OrderID:        uuid.NullUUID{UUID: orderID, Valid: true},
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create ledger entry: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to commit transaction: %v", err)
	}

	return &pb.ReleaseStockResponse{
		Success: true,
		Message: "stock released back successfully",
	}, nil
}
