package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"go.opentelemetry.io/otel"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	pbInventory "github.com/yourusername/project-backend-go/api/inventory"
	pbLogistics "github.com/yourusername/project-backend-go/api/logistics"
	"github.com/yourusername/project-backend-go/internal/config"
	inventorydb "github.com/yourusername/project-backend-go/internal/database/inventory"
	"github.com/yourusername/project-backend-go/internal/middleware"
	"github.com/yourusername/project-backend-go/internal/telemetry"
)

type mdCarrier struct {
	md metadata.MD
}

func (c mdCarrier) Get(key string) string {
	values := c.md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (c mdCarrier) Set(key, value string) {
	c.md.Set(key, value)
}

func (c mdCarrier) Keys() []string {
	keys := make([]string, 0, len(c.md))
	for k := range c.md {
		keys = append(keys, k)
	}
	return keys
}

func injectTraceGRPC(ctx context.Context) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.New(nil)
	}
	otel.GetTextMapPropagator().Inject(ctx, mdCarrier{md: md})
	return metadata.NewOutgoingContext(ctx, md)
}

func main() {
	cfg := config.LoadConfig()

	log.Println("Starting API Gateway...")

	// OpenTelemetry init
	ctx := context.Background()
	tp, shutdown, err := telemetry.InitTracer(ctx, "api-gateway", cfg.JaegerURL)
	if err != nil {
		log.Printf("Warning: failed to init telemetry: %v", err)
	} else {
		defer shutdown(ctx)
		_ = tp
	}

	// Connect to postgres (inventory_db) to authenticate users
	db, err := sql.Open("postgres", cfg.InventoryDBURL)
	if err != nil {
		log.Fatalf("failed to connect to User DB: %v", err)
	}
	defer db.Close()
	queries := inventorydb.New(db)

	// Dial gRPC Services
	invConn, err := grpc.Dial("localhost:"+cfg.InventoryPort, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to dial Inventory Service: %v", err)
	}
	defer invConn.Close()
	inventoryClient := pbInventory.NewInventoryServiceClient(invConn)

	logConn, err := grpc.Dial("localhost:"+cfg.LogisticsPort, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("failed to dial Logistics Service: %v", err)
	}
	defer logConn.Close()
	logisticsClient := pbLogistics.NewLogisticsServiceClient(logConn)

	// Auth Middleware
	authMid := middleware.NewAuthMiddleware(cfg.JWTSecret)

	r := gin.Default()

	// Public Routes
	r.POST("/register", func(c *gin.Context) {
		var req struct {
			Username string `json:"username" binding:"required"`
			Password string `json:"password" binding:"required"`
			Role     string `json:"role" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
			return
		}

		user, err := queries.CreateUser(c.Request.Context(), inventorydb.CreateUserParams{
			Username:     req.Username,
			PasswordHash: string(hash),
			Role:         req.Role,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "username already exists or invalid payload"})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"id":       user.ID.String(),
			"username": user.Username,
			"role":     user.Role,
		})
	})

	r.POST("/login", func(c *gin.Context) {
		var req struct {
			Username string `json:"username" binding:"required"`
			Password string `json:"password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		user, err := queries.GetUserByUsername(c.Request.Context(), req.Username)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
			return
		}

		accessToken, refreshToken, err := authMid.GenerateTokens(user.ID.String(), user.Role)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate tokens"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"access_token":  accessToken,
			"refresh_token": refreshToken,
		})
	})

	// Protected Routes
	protected := r.Group("/")
	protected.Use(authMid.Authenticate())

	protected.POST("/order", func(c *gin.Context) {
		tr := otel.Tracer("api-gateway")
		ctx, span := tr.Start(c.Request.Context(), "PlaceOrderHandler")
		defer span.End()

		var req struct {
			ItemID      string `json:"item_id" binding:"required"`
			Quantity    int32  `json:"quantity" binding:"required"`
			Destination string `json:"destination" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		orderID := uuid.New().String()

		// Call Inventory gRPC service with trace context
		gRPCCtx := injectTraceGRPC(ctx)
		res, err := inventoryClient.ReserveStock(gRPCCtx, &pbInventory.ReserveStockRequest{
			ItemId:      req.ItemID,
			Quantity:    req.Quantity,
			OrderId:     orderID,
			Destination: req.Destination,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if !res.Success {
			c.JSON(http.StatusConflict, gin.H{
				"order_id": orderID,
				"success":  false,
				"message":  res.Message,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"order_id": orderID,
			"success":  true,
			"message":  "order placed, shipment planning in progress",
		})
	})

	protected.GET("/shipments/:id", func(c *gin.Context) {
		tr := otel.Tracer("api-gateway")
		ctx, span := tr.Start(c.Request.Context(), "GetShipmentStatusHandler")
		defer span.End()

		shipmentID := c.Param("id")

		gRPCCtx := injectTraceGRPC(ctx)
		res, err := logisticsClient.GetShipmentStatus(gRPCCtx, &pbLogistics.GetShipmentStatusRequest{
			ShipmentId: shipmentID,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"shipment_id":     res.ShipmentId,
			"status":          res.Status,
			"carrier":         res.Carrier,
			"tracking_number": res.TrackingNumber,
		})
	})

	log.Printf("API Gateway listening on HTTP port %s", cfg.GatewayPort)
	r.Run(":" + cfg.GatewayPort)
}
