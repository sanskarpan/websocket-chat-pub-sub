package logging

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/websocket-chat/internal/config"
)

type Closer interface {
	Close() error
}

type nopCloser struct{}

func (n nopCloser) Close() error { return nil }

func NewLogger(cfg *config.Config) *zerolog.Logger {
	var output io.Writer = os.Stdout
	var closer Closer = nopCloser{}

	if cfg.Observability.Logging.Output == "file" && cfg.Observability.Logging.FilePath != "" {
		file, err := os.OpenFile(
			cfg.Observability.Logging.FilePath,
			os.O_APPEND|os.O_CREATE|os.O_WRONLY,
			0644,
		)
		if err == nil {
			output = file
			closer = file
		}
	}

	var logger zerolog.Logger
	if cfg.Observability.Logging.Format == "json" {
		logger = zerolog.New(output).
			Level(parseLevel(cfg.Observability.Logging.Level)).
			With().
			Timestamp().
			Str("app", cfg.App.Name).
			Str("version", cfg.App.Version).
			Str("environment", cfg.App.Environment).
			Logger()
	} else {
		consoleWriter := zerolog.ConsoleWriter{
			Out:        output,
			TimeFormat: time.RFC3339,
		}
		logger = zerolog.New(consoleWriter).
			Level(parseLevel(cfg.Observability.Logging.Level)).
			With().
			Timestamp().
			Str("app", cfg.App.Name).
			Str("version", cfg.App.Version).
			Str("environment", cfg.App.Environment).
			Logger()
	}

	_ = closer
	return &logger
}

func parseLevel(level string) zerolog.Level {
	l, err := zerolog.ParseLevel(level)
	if err != nil {
		return zerolog.InfoLevel
	}
	return l
}
