package log

import (
	"context"
	"fmt"
	"log/slog"
)

func Debugf(format string, args ...any) { logf(slog.LevelDebug, format, args...) }

func Infof(format string, args ...any) { logf(slog.LevelInfo, format, args...) }

func Warnf(format string, args ...any) { logf(slog.LevelWarn, format, args...) }

func Errorf(format string, args ...any) { logf(slog.LevelError, format, args...) }

func logf(level slog.Level, format string, args ...any) {
	if slog.Default().Enabled(context.Background(), level) { // skips Sprintf for disabled levels
		slog.Log(context.Background(), level, fmt.Sprintf(format, args...))
	}
}
