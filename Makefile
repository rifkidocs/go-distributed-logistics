# Makefile for Distributed Warehouse & Logistics API

.PHONY: all proto sqlc dev-gateway dev-inventory dev-logistics compose-up compose-down

all: proto sqlc

# Generate Go code from Protocol Buffers using Docker
proto:
	@echo "Generating Protobuf files..."
	docker run --rm -v "$$(pwd):/workspace" -w /workspace namely/protoc-go -f api/inventory/inventory.proto -l go -o . --go-source-relative
	docker run --rm -v "$$(pwd):/workspace" -w /workspace namely/protoc-go -f api/logistics/logistics.proto -l go -o . --go-source-relative

# Generate Go code from SQL files using sqlc Docker image
sqlc:
	@echo "Generating SQL database helper files using sqlc..."
	docker run --rm -v "$$(pwd):/src" -w /src sqlc/sqlc:1.26.0 generate

compose-up:
	docker compose up -d

compose-down:
	docker compose down
