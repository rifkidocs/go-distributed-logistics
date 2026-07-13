package services

import (
	"context"
	"database/sql"
	"fmt"


	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/yourusername/project-backend-go/api/logistics"
	logisticsdb "github.com/yourusername/project-backend-go/internal/database/logistics"
)

type LogisticsService struct {
	pb.UnimplementedLogisticsServiceServer
	db      *sql.DB
	queries logisticsdb.Querier
}

// NewLogisticsService menginisialisasi instance baru dari LogisticsService.
func NewLogisticsService(db *sql.DB) *LogisticsService {
	return &LogisticsService{
		db:      db,
		queries: logisticsdb.New(db),
	}
}

// CreateShipment membuat data pengiriman baru dengan status awal PENDING.
func (s *LogisticsService) CreateShipment(ctx context.Context, req *pb.CreateShipmentRequest) (*pb.CreateShipmentResponse, error) {
	tr := otel.Tracer("logistics-service")
	ctx, span := tr.Start(ctx, "CreateShipment")
	defer span.End()

	orderID, err := uuid.Parse(req.OrderId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid order ID: %v", err)
	}
	itemID, err := uuid.Parse(req.ItemId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid item ID: %v", err)
	}

	shipment, err := s.queries.CreateShipment(ctx, logisticsdb.CreateShipmentParams{
		OrderID:     orderID,
		ItemID:      itemID,
		Quantity:    req.Quantity,
		Destination: req.Destination,
		Status:      "PENDING",
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create shipment: %v", err)
	}

	return &pb.CreateShipmentResponse{
		ShipmentId: shipment.ID.String(),
		Status:     shipment.Status,
		Message:    "shipment created and pending carrier assignment",
	}, nil
}

// GetShipmentStatus mengambil status terkini dari pengiriman beserta informasi kurir & nomor resi.
func (s *LogisticsService) GetShipmentStatus(ctx context.Context, req *pb.GetShipmentStatusRequest) (*pb.GetShipmentStatusResponse, error) {
	tr := otel.Tracer("logistics-service")
	ctx, span := tr.Start(ctx, "GetShipmentStatus")
	defer span.End()

	shipmentID, err := uuid.Parse(req.ShipmentId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid shipment ID: %v", err)
	}

	shipment, err := s.queries.GetShipment(ctx, shipmentID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, status.Errorf(codes.NotFound, "shipment not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to query shipment: %v", err)
	}

	return &pb.GetShipmentStatusResponse{
		ShipmentId:     shipment.ID.String(),
		Status:         shipment.Status,
		Carrier:        shipment.Carrier.String,
		TrackingNumber: shipment.TrackingNumber.String,
	}, nil
}

// UpdateShipmentStatus memperbarui status perjalanan dari pengiriman tertentu.
func (s *LogisticsService) UpdateShipmentStatus(ctx context.Context, req *pb.UpdateShipmentStatusRequest) (*pb.UpdateShipmentStatusResponse, error) {
	tr := otel.Tracer("logistics-service")
	ctx, span := tr.Start(ctx, "UpdateShipmentStatus")
	defer span.End()

	shipmentID, err := uuid.Parse(req.ShipmentId)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid shipment ID: %v", err)
	}

	_, err = s.queries.UpdateShipmentStatus(ctx, logisticsdb.UpdateShipmentStatusParams{
		ID:     shipmentID,
		Status: req.Status,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update shipment status: %v", err)
	}

	return &pb.UpdateShipmentStatusResponse{
		Success: true,
		Message: fmt.Sprintf("shipment status updated to %s", req.Status),
	}, nil
}

// AssignCarrier mendaftarkan kurir dan nomor resi pengiriman ke database logistik.
func (s *LogisticsService) AssignCarrier(ctx context.Context, shipmentID uuid.UUID, carrier string, trackingNum string) error {
	_, err := s.queries.AssignCarrier(ctx, logisticsdb.AssignCarrierParams{
		ID:             shipmentID,
		Carrier:        sql.NullString{String: carrier, Valid: true},
		TrackingNumber: sql.NullString{String: trackingNum, Valid: true},
	})
	return err
}
