package config

import (
	"os"
)

type Config struct {
	GatewayPort   string
	InventoryPort string
	LogisticsPort string

	InventoryDBURL string
	LogisticsDBURL string

	RedisURL string
	NatsURL  string
	JaegerURL string

	JWTSecret string
}

func LoadConfig() *Config {
	return &Config{
		GatewayPort:    getEnv("GATEWAY_PORT", "8080"),
		InventoryPort:  getEnv("INVENTORY_PORT", "8081"),
		LogisticsPort:  getEnv("LOGISTICS_PORT", "8082"),
		InventoryDBURL: getEnv("INVENTORY_DB_URL", "postgres://postgres:password@localhost:5431/inventory_db?sslmode=disable"),
		LogisticsDBURL: getEnv("LOGISTICS_DB_URL", "postgres://postgres:password@localhost:5433/logistics_db?sslmode=disable"),
		RedisURL:       getEnv("REDIS_URL", "localhost:6379"),
		NatsURL:        getEnv("NATS_URL", "nats://localhost:4222"),
		JaegerURL:      getEnv("JAEGER_URL", "http://localhost:4317"), // OTLP gRPC port
		JWTSecret:      getEnv("JWT_SECRET", "super-secret-warehouse-key"),
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
