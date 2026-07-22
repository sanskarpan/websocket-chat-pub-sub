package tracing

import (
	"context"
	"github.com/websocket-chat/internal/config"
)

type Tracer struct {
	enabled bool
}

func (t *Tracer) Shutdown(ctx context.Context) error {
	return nil
}

func InitTracer(cfg *config.Config) (*Tracer, error) {
	return &Tracer{
		enabled: cfg.Observability.Tracing.Enabled,
	}, nil
}

func StartSpan(ctx context.Context, name string) (context.Context, func()) {
	return ctx, func() {}
}
