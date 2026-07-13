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

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/rifkidocs/go-distributed-logistics/docs"
	pbInventory "github.com/rifkidocs/go-distributed-logistics/api/inventory"
	pbLogistics "github.com/rifkidocs/go-distributed-logistics/api/logistics"
	"github.com/rifkidocs/go-distributed-logistics/internal/config"
	inventorydb "github.com/rifkidocs/go-distributed-logistics/internal/database/inventory"
	"github.com/rifkidocs/go-distributed-logistics/internal/middleware"
	"github.com/rifkidocs/go-distributed-logistics/internal/telemetry"
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

// APIResponse is the unified best-practice structure for all REST API responses
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Errors  []string    `json:"errors,omitempty"`
}

func SendSuccess(c *gin.Context, statusCode int, message string, data interface{}) {
	c.JSON(statusCode, APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

func SendError(c *gin.Context, statusCode int, message string, errs ...string) {
	c.JSON(statusCode, APIResponse{
		Success: false,
		Message: message,
		Errors:  errs,
	})
}

type RegisterInput struct {
	Username string `json:"username" binding:"required" example:"john_doe"`
	Password string `json:"password" binding:"required" example:"securepass123"`
	Role     string `json:"role" binding:"required" example:"staff"`
}

type LoginInput struct {
	Username string `json:"username" binding:"required" example:"john_doe"`
	Password string `json:"password" binding:"required" example:"securepass123"`
}

type OrderInput struct {
	ItemID      string `json:"item_id" binding:"required" example:"11111111-1111-1111-1111-111111111111"`
	Quantity    int32  `json:"quantity" binding:"required" example:"1"`
	Destination string `json:"destination" binding:"required" example:"Jakarta Warehouse"`
}

// @title Distributed Warehouse & Logistics API
// @version 1.0
// @description High-performance distributed warehouse and logistics microservices gateway.
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8080
// @BasePath /
// @query.collection.format multi

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Input authorization token with format "Bearer <JWT_TOKEN>"
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

	// Swagger documentation endpoint
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Public Routes
	r.POST("/register", RegisterHandler(queries))
	r.POST("/login", LoginHandler(queries, authMid))

	// Protected Routes
	protected := r.Group("/")
	protected.Use(authMid.Authenticate())

	protected.POST("/order", PlaceOrderHandler(inventoryClient))
	protected.GET("/shipments/:id", GetShipmentStatusHandler(logisticsClient))

	log.Printf("API Gateway listening on HTTP port %s", cfg.GatewayPort)
	r.Run(":" + cfg.GatewayPort)
}

// RegisterHandler registers a new user
// @Summary Register a new user
// @Description Create a user credentials and database entry with staff/admin role
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body RegisterInput true "Registration credentials"
// @Success 201 {object} APIResponse "Registration Successful"
// @Failure 400 {object} APIResponse "Invalid input or username exists"
// @Failure 500 {object} APIResponse "Internal server error"
// @Router /register [post]
func RegisterHandler(queries inventorydb.Querier) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req RegisterInput
		if err := c.ShouldBindJSON(&req); err != nil {
			SendError(c, http.StatusBadRequest, "Invalid request payload", err.Error())
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			SendError(c, http.StatusInternalServerError, "Failed to process request")
			return
		}

		user, err := queries.CreateUser(c.Request.Context(), inventorydb.CreateUserParams{
			Username:     req.Username,
			PasswordHash: string(hash),
			Role:         req.Role,
		})
		if err != nil {
			SendError(c, http.StatusBadRequest, "Registration failed", "Username already exists or database error")
			return
		}

		SendSuccess(c, http.StatusCreated, "User registered successfully", gin.H{
			"id":       user.ID.String(),
			"username": user.Username,
			"role":     user.Role,
		})
	}
}

// LoginHandler authenticates the user
// @Summary Log in to retrieve JWT access tokens
// @Description Logs user in using standard username/password and retrieves JWT tokens
// @Tags Authentication
// @Accept json
// @Produce json
// @Param input body LoginInput true "Login credentials"
// @Success 200 {object} APIResponse "Authentication Successful"
// @Failure 400 {object} APIResponse "Invalid input format"
// @Failure 401 {object} APIResponse "Unauthorized - invalid credentials"
// @Failure 500 {object} APIResponse "Internal server error"
// @Router /login [post]
func LoginHandler(queries inventorydb.Querier, authMid *middleware.AuthMiddleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req LoginInput
		if err := c.ShouldBindJSON(&req); err != nil {
			SendError(c, http.StatusBadRequest, "Invalid login payload", err.Error())
			return
		}

		user, err := queries.GetUserByUsername(c.Request.Context(), req.Username)
		if err != nil {
			SendError(c, http.StatusUnauthorized, "Invalid credentials", "Invalid username or password")
			return
		}

		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
			SendError(c, http.StatusUnauthorized, "Invalid credentials", "Invalid username or password")
			return
		}

		accessToken, refreshToken, err := authMid.GenerateTokens(user.ID.String(), user.Role)
		if err != nil {
			SendError(c, http.StatusInternalServerError, "Failed to authenticate user")
			return
		}

		SendSuccess(c, http.StatusOK, "Authentication successful", gin.H{
			"access_token":  accessToken,
			"refresh_token": refreshToken,
		})
	}
}

// PlaceOrderHandler reserves inventory stock
// @Summary Place a warehouse order
// @Description Locks and reserves stock for item ID and triggers logistics lifecycle asynchronously
// @Tags Orders
// @Accept json
// @Produce json
// @Param Authorization header string true "Insert JWT Bearer token" default(Bearer <JWT>)
// @Param input body OrderInput true "Order placement attributes"
// @Security BearerAuth
// @Success 200 {object} APIResponse "Order placed successfully"
// @Failure 400 {object} APIResponse "Invalid order format"
// @Failure 409 {object} APIResponse "Stock conflict / Out of stock"
// @Failure 500 {object} APIResponse "gRPC internal error"
// @Router /order [post]
func PlaceOrderHandler(inventoryClient pbInventory.InventoryServiceClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		tr := otel.Tracer("api-gateway")
		ctx, span := tr.Start(c.Request.Context(), "PlaceOrderHandler")
		defer span.End()

		var req OrderInput
		if err := c.ShouldBindJSON(&req); err != nil {
			SendError(c, http.StatusBadRequest, "Invalid order request", err.Error())
			return
		}

		orderID := uuid.New().String()

		gRPCCtx := injectTraceGRPC(ctx)
		res, err := inventoryClient.ReserveStock(gRPCCtx, &pbInventory.ReserveStockRequest{
			ItemId:      req.ItemID,
			Quantity:    req.Quantity,
			OrderId:     orderID,
			Destination: req.Destination,
		})
		if err != nil {
			SendError(c, http.StatusInternalServerError, "Inventory communication error", err.Error())
			return
		}

		if !res.Success {
			SendError(c, http.StatusConflict, "Order placement rejected", res.Message)
			return
		}

		SendSuccess(c, http.StatusOK, "Order placed successfully", gin.H{
			"order_id": orderID,
		})
	}
}

// GetShipmentStatusHandler checks delivery status
// @Summary Get status of a shipment
// @Description Retrieves active status, carrier, and tracking number for shipment ID
// @Tags Logistics
// @Accept json
// @Produce json
// @Param Authorization header string true "Insert JWT Bearer token" default(Bearer <JWT>)
// @Param id path string true "Shipment ID (UUID)"
// @Security BearerAuth
// @Success 200 {object} APIResponse "Shipment details fetched"
// @Failure 500 {object} APIResponse "gRPC internal error"
// @Router /shipments/{id} [get]
func GetShipmentStatusHandler(logisticsClient pbLogistics.LogisticsServiceClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		tr := otel.Tracer("api-gateway")
		ctx, span := tr.Start(c.Request.Context(), "GetShipmentStatusHandler")
		defer span.End()

		shipmentID := c.Param("id")

		gRPCCtx := injectTraceGRPC(ctx)
		res, err := logisticsClient.GetShipmentStatus(gRPCCtx, &pbLogistics.GetShipmentStatusRequest{
			ShipmentId: shipmentID,
		})
		if err != nil {
			SendError(c, http.StatusInternalServerError, "Logistics communication error", err.Error())
			return
		}

		SendSuccess(c, http.StatusOK, "Shipment details retrieved", gin.H{
			"shipment_id":     res.ShipmentId,
			"status":          res.Status,
			"carrier":         res.Carrier,
			"tracking_number": res.TrackingNumber,
		})
	}
}
