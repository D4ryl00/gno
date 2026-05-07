package common

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/gnolang/gno/gno.land/pkg/log"
	"github.com/gnolang/gno/tm2/pkg/commands"
)

// Used to flush the logger.
type logFlusher func()

func LoggerFromServerFlags(serverFlags *ServerFlags, cmdIO commands.IO) (*slog.Logger, logFlusher, error) {
	out := cmdIO.Out()
	if out == nil {
		out = commands.WriteNopCloser(io.Discard)
	}

	// Initialize the zap logger.
	zapLogger, err := log.InitializeZapLogger(
		out,
		serverFlags.LogLevel,
		serverFlags.LogFormat,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to initialize zap logger: %w", err)
	}

	// Keep a reference to the zap logger flush function.
	flusher := func() { _ = zapLogger.Sync() }

	// Wrap the zap logger with a slog logger.
	logger := log.ZapLoggerToSlog(zapLogger)

	return logger, flusher, nil
}
