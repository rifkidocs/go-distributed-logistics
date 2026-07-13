# Spec: Distributed Warehouse & Logistics API

## Objective
Build a highly performant, distributed Warehouse & Logistics backend platform demonstrating modern microservices patterns in Go. The system will handle incoming client REST requests, delegate internal tasks via fast gRPC connections, publish events asynchronously using NATS JetStream, and guarantee consistency using distributed locking.

### Core User Stories / Features:
1. **API Gateway**: Public-facing REST endpoint for order placement, stock verification, and tracking. Handles authentication (JWT Access/Refresh tokens).
2. **Inventory Service**: Manages items, warehouses, and stock levels. Implements a double-entry stock ledger to log all stock modifications and uses Redis distributed locking to prevent stock reservation race conditions.
3. **Logistics Service**: Assigns shipments to carriers, manages tracking status, and publishes shipment updates asynchronously.
4. **Event-Driven Workflows**:
   - When an order is placed and stock is reserved -> Publish `stock.reserved` event -> Logistics Service starts shipment planning.
   - When a shipment status changes -> Publish `shipment.updated` event.
5. **Observability**: Centralized metrics, tracing, and logging to monitor performance and debug potential bottlenecks.

---

## Tech Stack
- **Go**: 1.22+
- **REST Gateway**: Gin Web Framework (v1.10+)
- **RPC Communication**: gRPC & Protocol Buffers (proto3)
- **Database**: PostgreSQL (v16+)
- **SQL Code Generator**: `sqlc` (compiles raw SQL queries into type-safe Go code)
- **Database Migrations**: `golang-migrate`
- **Caching & Lock Manager**: Redis (v7+) using `go-redis` and `redsync`
- **Message Broker**: NATS JetStream (lightweight, high-performance Go-native streaming)
- **Observability**: 
  - **OpenTelemetry (OTel)** + **Jaeger**: Distributed tracing across gRPC calls
  - **Prometheus** & **Grafana**: Metrics collection and dashboarding
  - **Loki** + **Promtail**: Centralized log aggregation

---

## Commands
```bash
# Infrastructure
docker-compose up -d       # Launch databases, message broker, and observability tools
docker-compose down        # Stop all infrastructure containers

# Codegen & Database Tools
make proto                 # Generate Go files from Protocol Buffer definitions
make sqlc                  # Generate Go query files from SQL schema & queries
migrate -path db/migrations -database "postgres://localhost:5432/db" up

# Execution (Development)
go run cmd/gateway/main.go
go run cmd/inventory/main.go
go run cmd/logistics/main.go

# Verification
go test ./... -v           # Run all unit and integration tests
go vet ./...               # Go static analysis
golangci-lint run          # Linting
```

---

## Project Structure
We will use a single Go module monorepo structure to keep development simple while maintaining distinct boundaries between service directories.

```
/
├── api/                   # Protocol Buffer (.proto) definitions and generated files
│   ├── inventory/
│   └── logistics/
├── cmd/                   # Application entry points
│   ├── gateway/           # REST API Gateway (Gin)
│   ├── inventory/         # Inventory Service (gRPC + Postgres + Redis)
│   └── logistics/         # Logistics Service (gRPC + Postgres + NATS)
├── db/                    # SQL schemas, migrations, and sqlc configurations
│   ├── migrations/        # SQL migration files
│   └── queries/           # SQL queries for sqlc compilation
├── internal/              # Shared libraries (internal to the project)
│   ├── config/            # Environment variable configuration loading
│   ├── database/          # Generated sqlc code
│   ├── event/             # NATS publisher/subscriber wrappers
│   └── telemetry/         # OpenTelemetry tracer & meter initialization
├── docker-compose.yml     # Local environment infrastructure
├── Makefile               # Build and codegen automation rules
└── go.mod                 # Go module definition
```

---

## Code Style
We follow standard Go practices:
- **Clean Architecture / Domain separation**: Handlers -> Services -> Repositories.
- **Explicit Dependency Injection**: No global states or singletons inside packages. Pass dependencies (DB, Redis client, NATS conn) explicitly via constructors (`NewService(...)`).
- **Context propagation**: Always pass and propagate `context.Context` to DB calls, gRPC requests, and logger lines to ensure tracing works correctly.

### Example Code Style (Explicit Dependencies & Context Tracing):
```go
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/google/uuid"
)

type SqlcRepository struct {
	db *sql.DB // Explicit dependency
}

func NewRepository(db *sql.DB) *SqlcRepository {
	return &SqlcRepository{db: db}
}

func (r *SqlcRepository) ReserveStock(ctx context.Context, itemID uuid.UUID, qty int) error {
	// Execute within standard Go context containing trace span
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback()

	// Business logic / sqlc query here...
	return tx.Commit()
}
```

---

## Testing Strategy
1. **Unit Testing**: Standard Go testing library (`testing`) along with mocking tools (like `testify` or generated mocks) for testing business services in isolation.
2. **Integration Testing**: Spin up real PostgreSQL and Redis containers using Docker for local integration verification.
3. **E2E Testing**: Scripted REST requests hitting the API Gateway to verify the full flow (Gateway -> Inventory -> Logistics -> NATS publish).

---

## Boundaries
- **Always**:
  - Propagate `context.Context` to database and external gRPC/HTTP calls.
  - Wrap errors with useful context (`fmt.Errorf("failed to execute X: %w", err)`).
  - Release locks and close resources via `defer`.
- **Ask First**:
  - Adding external libraries not specified in the tech stack.
  - Modifying the protocol buffer schemas once finalized.
- **Never**:
  - Store plaintext credentials/secrets in git. Use `.env` or system variables.
  - Skip writing tests for core business validation logic.

---

## Success Criteria
- [ ] A functional `docker-compose.yml` spinning up PostgreSQL, Redis, NATS, Grafana, Loki, Prometheus, and Jaeger.
- [ ] All database schemas created and migrations runnable via a CLI command.
- [ ] A customer can call the REST API Gateway to order items:
  1. The API Gateway authenticates the request.
  2. Inventory Service verifies and reserves stock using Redis locks.
  3. Inventory Service records transactions in the ledger.
  4. NATS event `stock.reserved` is emitted.
  5. Logistics Service consumes the event and schedules shipment.
- [ ] Distributed tracing is visible in Jaeger when tracing a single REST Gateway request across the services.
- [ ] Prometheus metrics and Loki logs display correctly in Grafana dashboards.

---

## Open Questions
1. **How do you want to handle user authentication?** Should we implement a simple local JWT database table, or mock the OAuth2 server for simplicity?
2. **Would you like me to create a quick test client script (Python, Bash, or Go)** to simulate stock orders and visualize the traces and dashboard data?
