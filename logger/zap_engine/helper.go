package zap_engine

import (
	"context"

	loggerwrapper "github.com/PavelAgarkov/service-pkg/logger"
)

func FlushLogs() {
	Sync()
}

func WriteInfoLog(ctx context.Context, entry *loggerwrapper.LogEntry) {
	msg, fields := unpack(entry)
	Info(ctx, buildMessage(msg, "info", fields))
}

func WriteDebugLog(ctx context.Context, entry *loggerwrapper.LogEntry) {
	msg, fields := unpack(entry)
	Debug(ctx, buildMessage(msg, "debug", fields))
}

func WriteWarnLog(ctx context.Context, entry *loggerwrapper.LogEntry) {
	msg, fields := unpack(entry)
	Warn(ctx, buildMessage(msg, "warn", fields))
}

func WriteErrorLog(ctx context.Context, entry *loggerwrapper.LogEntry) {
	msg, fields := unpack(entry)
	Error(ctx, buildMessage(msg, "error", fields))
}

func WritePanicLog(ctx context.Context, entry *loggerwrapper.LogEntry) {
	msg, fields := unpack(entry)
	Panic(ctx, buildMessage(msg, "panic", fields))
}

func WriteFatalLog(ctx context.Context, entry *loggerwrapper.LogEntry) {
	msg, fields := unpack(entry)
	Fatal(ctx, buildMessage(msg, "fatal", fields))
}
