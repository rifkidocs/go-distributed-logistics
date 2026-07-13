# Distributed Warehouse & Logistics API

A highly performant, distributed Warehouse & Logistics backend platform demonstrating modern microservices patterns in Go. The system handles client REST requests, routes internal calls via gRPC, manages stock consistency using Redis distributed locking, and publishes logistics updates asynchronously using NATS JetStream.

---

## Tech Stack
- **Language**: Go (v1.22+)
- **API Gateway**: Gin Web Framework (REST) + JWT Authentication
- **Internal RPC**: gRPC & Protocol Buffers (proto3)
- **Database**: PostgreSQL (sqlc for compile-safe queries + golang-migrate)
- **Cache & Lock Manager**: Redis (Redsync distributed locks)
- **Message Broker**: NATS JetStream (asynchronous event-driven messaging)
- **Observability**: OpenTelemetry (OTel), Jaeger (Distributed Tracing), Prometheus, Loki, Grafana

---

## System Architecture

```
                      [ Client Requests ]
                              │
                              ▼
                ┌───────────────────────────┐
                │        API Gateway        │
                │     (Gin REST | JWT)      │
                └─────────────┬─────────────┘
                              │
                    gRPC (Trace Context)
                              │
             ┌────────────────┴────────────────┐
             ▼                                 ▼
┌───────────────────────────┐     ┌───────────────────────────┐
│     Inventory Service     │     │     Logistics Service     │
│   (gRPC | PostgreSQL)     │     │   (gRPC | PostgreSQL)     │
└────────────┬──────────────┘     └────────────▲──────────────┘
             │                                 │
     Redis Lock / Ledger                       │
             │                           NATS Subscription
             ▼                                 │
     [ NATS JetStream ] ───(stock.reserved)────┘
```

---

## How to Run the Project

### 1. Start Infrastructure Dependencies
Run all databases, NATS, and telemetry collectors in the background using Docker Compose:
```bash
docker compose up -d
```
Verify that all containers are healthy:
```bash
docker ps
```

### 2. Run Database Migrations
Initialize the database tables and schemas for both the Inventory and Logistics databases:
```bash
go run cmd/migrate/main.go
```

### 3. Run Microservices
Open three separate terminal windows/sessions and run the services in order:

* **Inventory Service**:
  ```bash
  go run cmd/inventory/main.go
  ```
  *(Listens on gRPC port `:8081` and seeds initial stock data).*

* **Logistics Service**:
  ```bash
  go run cmd/logistics/main.go
  ```
  *(Listens on gRPC port `:8082` and consumes NATS event streams).*

* **API Gateway Service**:
  ```bash
  go run cmd/gateway/main.go
  ```
  *(Listens on HTTP port `:8080` for user REST requests).*

---

## Testing & Verifying Flows

### Run the Load Simulator
We have built a simulator script to test registration, login, and high-concurrency ordering. Run:
```bash
go run cmd/simulator/main.go
```

The simulator will attempt to place 10 orders of 15 MacBook units concurrently. Since the initial stock is 100:
- **Exactly 6 orders** will succeed.
- **4 orders** will fail with `insufficient stock`.
- This verifies the **Redis Lock** is successfully preventing stock overselling (race conditions).

---

## Dashboards & Monitoring

Once the simulator runs, you can visually trace and monitor requests:

1. **Distributed Tracing (Jaeger)**:
   - Open **[http://localhost:16686](http://localhost:16686)**.
   - Choose `api-gateway` in the services dropdown and click **Find Traces**.
   - You can see the full trace of a single order starting from Gin HTTP -> gRPC Inventory -> NATS Publish -> NATS Consume -> gRPC Logistics!

2. **Metrics & Logging (Grafana)**:
   - Open **[http://localhost:3000](http://localhost:3000)** (Username: `admin`, Password: `admin`).
   - Monitor system logs (Loki) and endpoint metrics (Prometheus).
