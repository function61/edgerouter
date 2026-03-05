// short-time shim until we can migrate to newer `gokit` that handles this for us
package slogshim

import (
	"io"
	"log"
	"log/slog"
	"os"
)

func New() *slog.Logger {
	return NewWithOutput(os.Stderr)
}

func NewWithOutput(output io.Writer) *slog.Logger {
	logger := slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)
	return logger
}

func ToStd(logger *slog.Logger, level slog.Level) *log.Logger {
	return slog.NewLogLogger(logger.Handler(), level)
}
