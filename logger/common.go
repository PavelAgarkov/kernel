package logger_wrapper

import "time"

type LogEntry struct {
	Msg       string
	Args      any
	Result    any
	Error     error
	Component string
	Method    string
	Start     *time.Time
}
