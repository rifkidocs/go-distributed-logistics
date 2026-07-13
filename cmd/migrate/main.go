package main

import (
	"log"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	log.Println("Starting database migrations...")

	// Migrate Inventory DB
	log.Println("Migrating Inventory DB...")
	mInv, err := migrate.New(
		"file://db/migrations/inventory",
		"postgres://postgres:password@localhost:5431/inventory_db?sslmode=disable",
	)
	if err != nil {
		log.Fatalf("failed to initialize inventory migration: %v", err)
	}
	if err := mInv.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("failed to apply inventory migrations: %v", err)
	}
	log.Println("Inventory DB migration successful.")

	// Migrate Logistics DB
	log.Println("Migrating Logistics DB...")
	mLog, err := migrate.New(
		"file://db/migrations/logistics",
		"postgres://postgres:password@localhost:5433/logistics_db?sslmode=disable",
	)
	if err != nil {
		log.Fatalf("failed to initialize logistics migration: %v", err)
	}
	if err := mLog.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("failed to apply logistics migrations: %v", err)
	}
	log.Println("Logistics DB migration successful.")

	log.Println("All migrations completed successfully!")
}
