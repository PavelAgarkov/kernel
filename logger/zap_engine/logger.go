package zap_engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	loggerwrapper "github.com/PavelAgarkov/service-pkg/logger"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	log         = zap.NewNop()
	atomicLevel zap.AtomicLevel // для динамического изменения уровня
)

func InitLoggerForStdout(level zapcore.Level, cloud bool, cfg *zapcore.EncoderConfig, option ...zap.Option) error {
	atomicLevel = zap.NewAtomicLevelAt(level)

	var encCfg zapcore.EncoderConfig
	if cfg == nil {
		encCfg = zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			MessageKey:     "message",
			CallerKey:      "caller",
			StacktraceKey:  "stack",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     func(t time.Time, enc zapcore.PrimitiveArrayEncoder) { enc.AppendString(t.Format(time.RFC3339Nano)) },
			EncodeDuration: zapcore.StringDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		}
	} else {
		encCfg = *cfg
	}

	var enc zapcore.Encoder
	if cloud {
		enc = zapcore.NewJSONEncoder(encCfg)
	} else {
		enc = zapcore.NewConsoleEncoder(encCfg)
	}

	ws := zapcore.Lock(os.Stdout)

	core := zapcore.NewCore(
		enc,
		zapcore.AddSync(ws),
		atomicLevel,
	)

	opt := []zap.Option{
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
	}
	opt = append(opt, option...)

	log = zap.New(core, opt...)

	return nil
}

// SetLevel Позволяет менять уровень в рантайме
func SetLevel(level string) error {
	return atomicLevel.UnmarshalText([]byte(level))
}

func GetLevel() string {
	return atomicLevel.Level().String()
}

func Debug(ctx context.Context, msg string) {
	log.Debug(msg)
}
func Info(ctx context.Context, msg string) {
	log.Info(msg)
}
func Warn(ctx context.Context, msg string) {
	log.Warn(msg)
}
func Error(ctx context.Context, msg string) {
	log.Error(msg)
}
func Panic(ctx context.Context, msg string) {
	log.Panic(msg)
}
func Fatal(ctx context.Context, msg string) {
	log.Fatal(msg)
}

type Field struct {
	Key       string
	Type      zapcore.FieldType
	String    string
	Integer   int64
	Interface any
}

func WithField(key string, val any) Field {
	switch v := val.(type) {
	case string:
		return Field{Key: key, Type: zapcore.StringType, String: v}
	case bool:
		if v {
			return Field{Key: key, Type: zapcore.BoolType, Integer: 1}
		}
		return Field{Key: key, Type: zapcore.BoolType, Integer: 0}
	case int, int8, int16, int32, int64:
		return Field{Key: key, Type: zapcore.Int64Type, Integer: toI64(v)}
	case uint, uint8, uint16, uint32:
		return Field{Key: key, Type: zapcore.Uint64Type, Integer: int64(toU64(v))}
	case uint64:
		u := v
		if u > math.MaxInt64 {
			return Field{Key: key, Type: zapcore.Uint64Type, Interface: u}
		}
		return Field{Key: key, Type: zapcore.Uint64Type, Integer: int64(u)}
	case float32:
		return Field{Key: key, Type: zapcore.Float64Type, Integer: int64(math.Float64bits(float64(v)))}
	case float64:
		return Field{Key: key, Type: zapcore.Float64Type, Integer: int64(math.Float64bits(v))}
	case time.Duration:
		return Field{Key: key, Type: zapcore.DurationType, Integer: int64(v)}
	case error:
		return Field{Key: key, Type: zapcore.ErrorType, Interface: v}
	default:
		return Field{Key: key, Type: zapcore.ReflectType, Interface: v}
	}
}

func WithError(err error) Field {
	if err == nil {
		return Field{}
	}
	return Field{Key: "error", Type: zapcore.ErrorType, Interface: err}
}

func toI64(v any) int64 {
	switch t := v.(type) {
	case int:
		return int64(t)
	case int8:
		return int64(t)
	case int16:
		return int64(t)
	case int32:
		return int64(t)
	case int64:
		return t
	default:
		return 0
	}
}

func toU64(v any) uint64 {
	switch t := v.(type) {
	case uint:
		return uint64(t)
	case uint8:
		return uint64(t)
	case uint16:
		return uint64(t)
	case uint32:
		return uint64(t)
	case uint64:
		return t
	default:
		return 0
	}
}

func Sync() {
	if log == nil {
		return
	}
	if err := log.Sync(); err != nil && !isIgnorableSyncError(err) {
		fmt.Fprintf(os.Stderr, "zap sync error: %v\n", err)
	}
}

func isIgnorableSyncError(err error) bool {
	if err == nil {
		return true
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		switch pe.Err {
		case syscall.EINVAL, syscall.ENOTSUP, syscall.ENOSYS:
			return true
		}
	}
	return false
}

func buildMessage(msg string, level string, fields []Field) string {
	if len(fields) == 0 {
		return msg
	}

	var sb strings.Builder
	sb.Grow(len(msg) + 48)
	sb.WriteString(msg)

	first := true
	for _, f := range fields {
		k, v, ok := kv(f)
		if !ok {
			continue
		}
		if first {
			sb.WriteString(" | {")
			first = false
		} else {
			sb.WriteString(", ")
		}
		if k != "" {
			sb.WriteString(k)
			sb.WriteByte('=')
		}
		sb.WriteString(v)
	}

	sb.WriteString(", level->")
	sb.WriteString(level)

	if !first {
		sb.WriteByte('}')
	}
	return sb.String()
}

func kv(f Field) (string, string, bool) {
	switch f.Type {
	case zapcore.StringType:
		if f.String == "" {
			return "", "", false
		}
		return f.Key, f.String, true

	case zapcore.BoolType:
		return f.Key, strconv.FormatBool(f.Integer == 1), true

	case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type:
		return f.Key, strconv.FormatInt(f.Integer, 10), true

	case zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
		if f.Interface != nil {
			return f.Key, fmt.Sprint(f.Interface), true
		}

		return f.Key, strconv.FormatInt(f.Integer, 10), true

	case zapcore.Float64Type:
		v := math.Float64frombits(uint64(f.Integer))
		return f.Key, strconv.FormatFloat(v, 'f', -1, 64), true

	case zapcore.DurationType:
		return f.Key, time.Duration(f.Integer).String(), true

	case zapcore.ErrorType, zapcore.ReflectType, zapcore.StringerType:
		if f.Interface == nil {
			return "", "", false
		}
		return f.Key, fmt.Sprint(f.Interface), true

	default:
		switch {
		case f.Interface != nil:
			return f.Key, fmt.Sprint(f.Interface), true
		case f.String != "":
			return f.Key, f.String, true
		case f.Integer != 0:
			return f.Key, strconv.FormatInt(f.Integer, 10), true
		default:
			return "", "", false
		}
	}
}

func unpack(entry *loggerwrapper.LogEntry) (string, []Field) {
	if entry.Start == nil {
		return entry.Msg, []Field{
			WithField("component", entry.Component),
			WithField("method", entry.Method),
			WithField("args", entry.Args),
			WithField("result", entry.Result),
			WithField("latency", ""),
			WithError(entry.Error),
		}
	}
	return entry.Msg, []Field{
		WithField("component", entry.Component),
		WithField("method", entry.Method),
		WithField("args", entry.Args),
		WithField("result", entry.Result),
		WithField("latency", fmt.Sprintf("%v ms", time.Since(*entry.Start).Milliseconds())),
		WithError(entry.Error),
	}
}
