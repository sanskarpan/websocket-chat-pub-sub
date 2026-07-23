package tracing

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/websocket-chat/internal/config"
)

type spanKey struct{}

type span struct {
	name      string
	startTime time.Time
}

type Tracer struct {
	enabled bool
}

var globalTracer = &Tracer{enabled: false}

func (t *Tracer) Enabled() bool {
	return t.enabled
}

func (t *Tracer) Shutdown(ctx context.Context) error {
	return nil
}

func (t *Tracer) Start(ctx context.Context, name string) (context.Context, func()) {
	if !t.enabled {
		return ctx, func() {}
	}
	sp := &span{name: name, startTime: time.Now()}
	ctx = context.WithValue(ctx, spanKey{}, sp)
	log.Debug().Str("span", name).Msg("span started")
	return ctx, func() {
		duration := time.Since(sp.startTime)
		log.Debug().Str("span", name).Float64("duration_ms", float64(duration.Microseconds())/1000).Msg("span ended")
	}
}

func InitTracer(cfg *config.Config) (*Tracer, error) {
	globalTracer = &Tracer{
		enabled: cfg.Observability.Tracing.Enabled,
	}
	return globalTracer, nil
}

func StartSpan(ctx context.Context, name string) (context.Context, func()) {
	return globalTracer.Start(ctx, name)
}
