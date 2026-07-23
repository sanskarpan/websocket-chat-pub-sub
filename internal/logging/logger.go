package logging

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/websocket-chat/internal/config"
)

type LoggerHandle struct {
	Logger *zerolog.Logger
	Closer io.Closer
}

func NewLogger(cfg *config.Config) *LoggerHandle {
	var output io.Writer = os.Stdout
	var closer io.Closer = nopCloser{}

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

	return &LoggerHandle{
		Logger: &logger,
		Closer: closer,
	}
}

type nopCloser struct{}

func (n nopCloser) Close() error { return nil }

func parseLevel(level string) zerolog.Level {
	l, err := zerolog.ParseLevel(level)
	if err != nil {
		return zerolog.InfoLevel
	}
	return l
}
