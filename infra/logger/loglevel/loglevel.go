package loglevel

type LOG_LEVEL string

const (
	TRACE LOG_LEVEL = "TRACE"
	DEBUG LOG_LEVEL = "DEBUG"
	INFO  LOG_LEVEL = "INFO"
	WARN  LOG_LEVEL = "WARN"
	ERROR LOG_LEVEL = "ERROR"
	Fatal LOG_LEVEL = "Fatal"
)

func (d LOG_LEVEL) GetIndex() int {
	switch d {
	case TRACE:
		return 0
	case DEBUG:
		return 1
	case INFO:
		return 2
	case WARN:
		return 3
	case ERROR:
		return 4
	case Fatal:
		return 5
		
	}

	return 100
}
