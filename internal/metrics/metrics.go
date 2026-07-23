package metrics

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	WebsocketConnectionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "websocket_connections_active",
		Help: "Number of active WebSocket connections",
	})

	MessagesSentTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "websocket_messages_sent_total",
		Help: "Total number of messages sent",
	}, []string{"room_type"})

	MessagesReceivedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "websocket_messages_received_total",
		Help: "Total number of messages received",
	}, []string{"room_type"})

	ConnectionErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "websocket_connection_errors_total",
		Help: "Total number of WebSocket connection errors",
	}, []string{"error_type"})

	RoomSubscriptionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "room_subscriptions_active",
		Help: "Number of active room subscriptions",
	})

	AuthAttemptsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "auth_attempts_total",
		Help: "Total number of authentication attempts",
	}, []string{"status"})

	RateLimitedRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "rate_limited_requests_total",
		Help: "Total number of rate-limited requests",
	}, []string{"key"})
)

var (
	DBQueryDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "db_query_duration_seconds",
		Help:    "Duration of database queries",
		Buckets: prometheus.DefBuckets,
	}, []string{"query_type"})

	RedisOperationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "redis_operation_duration_seconds",
		Help:    "Duration of Redis operations",
		Buckets: prometheus.DefBuckets,
	}, []string{"operation"})
)

var (
	registerOnce sync.Once
	registerErr  error
)

type MetricsServer struct {
	port   int
	server *http.Server
}

func NewMetricsServer(port int) *MetricsServer {
	return &MetricsServer{port: port}
}

func (m *MetricsServer) Start() error {
	registerOnce.Do(func() {
		collectors := []prometheus.Collector{
			WebsocketConnectionsActive,
			MessagesSentTotal,
			MessagesReceivedTotal,
			ConnectionErrorsTotal,
			RoomSubscriptionsActive,
			AuthAttemptsTotal,
			RateLimitedRequestsTotal,
			DBQueryDuration,
			RedisOperationDuration,
		}

		for _, c := range collectors {
			if err := prometheus.Register(c); err != nil {
				if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
					registerErr = err
					return
				}
			}
		}
	})

	if registerErr != nil {
		return registerErr
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	m.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", m.port),
		Handler: mux,
	}

	return m.server.ListenAndServe()
}

func (m *MetricsServer) Shutdown(ctx context.Context) error {
	if m.server != nil {
		return m.server.Shutdown(ctx)
	}
	return nil
}
