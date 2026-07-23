package tracing

import (
	"context"
	"github.com/websocket-chat/internal/config"
)

type Tracer struct {
	enabled bool
}

func (t *Tracer) Enabled() bool {
	return t.enabled
}

func (t *Tracer) Shutdown(ctx context.Context) error {
	return nil
}

func (t *Tracer) Start(ctx context.Context, name string) (context.Context, func()) {
	return ctx, func() {}
}

func InitTracer(cfg *config.Config) (*Tracer, error) {
	return &Tracer{
		enabled: false,
	}, nil
}

func StartSpan(ctx context.Context, name string) (context.Context, func()) {
	return ctx, func() {}
}
