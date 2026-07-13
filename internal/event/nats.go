package event

import (
	"context"
	"fmt"
	"log"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

type JetStreamManager struct {
	NC nats.JetStreamContext
}

func NewJetStreamManager(natsURL string) (*JetStreamManager, *nats.Conn, error) {
	nc, err := nats.Connect(natsURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("failed to load JetStream context: %w", err)
	}

	// Create a stream if it doesn't exist
	streamName := "WAREHOUSE"
	_, err = js.StreamInfo(streamName)
	if err != nil {
		log.Printf("Creating JetStream stream: %s", streamName)
		_, err = js.AddStream(&nats.StreamConfig{
			Name:     streamName,
			Subjects: []string{"stock.*", "shipment.*"},
		})
		if err != nil {
			nc.Close()
			return nil, nil, fmt.Errorf("failed to create JetStream stream: %w", err)
		}
	}

	return &JetStreamManager{NC: js}, nc, nil
}

// InjectTraceContext injects OpenTelemetry trace ID into NATS message headers
func InjectTraceContext(ctx context.Context, msg *nats.Msg) {
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(msg.Header))
}

// ExtractTraceContext extracts trace ID from NATS message headers to allow tracing downstream services
func ExtractTraceContext(ctx context.Context, msg *nats.Msg) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(msg.Header))
}
