# Tasks: Distributed Warehouse & Logistics API

Here is the checklist of implementable tasks to build the project.

## Phase 1: Infrastructure Setup & Schemas
- [x] **Task 1.1: Docker Compose Setup**
  - **Acceptance**: Run `docker-compose up -d` and verify Postgres, Redis, NATS, Grafana, Loki, Prometheus, and Jaeger start.
  - **Verify**: `docker ps` shows all containers running.
  - **Files**: `docker-compose.yml`
- [x] **Task 1.2: Proto Definitions**
  - **Acceptance**: Define gRPC schemas for Inventory and Logistics. Compile them to Go.
  - **Verify**: Compiled `.pb.go` files are generated.
  - **Files**: `api/inventory/inventory.proto`, `api/logistics/logistics.proto`, `Makefile`

## Phase 2: Core Shared Libraries & DB Migrations
- [x] **Task 2.1: Database Migrations**
  - **Acceptance**: Set up migrations for postgres databases (users, items, ledger, shipments).
  - **Verify**: Apply migrations successfully.
  - **Files**: `db/migrations/*.sql`
- [x] **Task 2.2: sqlc Setup & DB Generation**
  - **Acceptance**: Define SQL queries for database operations and compile them to Go.
  - **Verify**: Run `sqlc generate` with no errors.
  - **Files**: `db/sqlc.yaml`, `db/queries/*.sql`
- [x] **Task 2.3: Shared telemetry & NATS config**
  - **Acceptance**: Create boilerplate for OpenTelemetry initialization, configuration, and NATS JetStream connectivity.
  - **Verify**: Package compiles.
  - **Files**: `internal/config/config.go`, `internal/telemetry/telemetry.go`, `internal/event/nats.go`

## Phase 3: Services Implementation
- [x] **Task 3.1: Inventory Service (gRPC + Redis Lock)**
  - **Acceptance**: Implement gRPC server endpoints for checking stock, reserving stock (with Redis lock), and logging ledger transactions.
  - **Verify**: Run unit/integration tests for Stock reservation under race conditions.
  - **Files**: `cmd/inventory/main.go`, `internal/services/inventory.go`
- [x] **Task 3.2: Logistics Service & Event Consumer**
  - **Acceptance**: Implement gRPC server for shipments. Listen to NATS `stock.reserved` event to auto-create shipments.
  - **Verify**: NATS publisher in test triggers shipment creation.
  - **Files**: `cmd/logistics/main.go`, `internal/services/logistics.go`

## Phase 4: REST API Gateway & Observability
- [x] **Task 4.1: Gateway Service (Gin + Auth Middleware)**
  - **Acceptance**: Implement REST endpoints for login, registration, ordering products, and tracking shipments. Integrate JWT verification and trace propagation.
  - **Verify**: `curl` requests return correct authenticated and routed responses.
  - **Files**: `cmd/gateway/main.go`, `internal/middleware/auth.go`
- [x] **Task 4.2: Grafana & Loki Dashboards**
  - **Acceptance**: Set up Prometheus metrics export and Loki log integration. Run simulated script and observe dashboards.
  - **Verify**: Open Grafana at `http://localhost:3000` and view trace graphs and logs.
  - **Files**: `docker/grafana/...`, `cmd/simulator/main.go`
