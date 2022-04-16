package logg

import "fmt"

const (
	LevelInfo Level = iota
	LevelWarning
	LevelDebug
)

type Level int

func (l Level) String() string {
	switch l {
	case LevelInfo:
		return "Info"
	case LevelWarning:
		return "Warning"
	case LevelDebug:
		return "Debug"
	default:
		return "Unknown"
	}
}

type LogEntry struct {
	Message string
	Level   Level
}

func (e LogEntry) String() string {
	switch e.Level {
	case LevelWarning:
		return fmt.Sprintf("[WARNING] %s", e.Message)
	case LevelDebug:
		return fmt.Sprintf("[DEBUG] %s", e.Message)
	default:
		return e.Message
	}

}

func Info(message string) LogEntry {
	return LogEntry{Message: message, Level: LevelInfo}
}

func Infof(format string, a ...interface{}) LogEntry {
	return LogEntry{Message: fmt.Sprintf(format, a...), Level: LevelInfo}
}

func Warning(message string) LogEntry {
	return LogEntry{Message: message, Level: LevelWarning}
}

func Warningf(format string, a ...interface{}) LogEntry {
	return LogEntry{Message: fmt.Sprintf(format, a...), Level: LevelWarning}
}

func Debug(message string) LogEntry {
	return LogEntry{Message: message, Level: LevelDebug}
}

func Debugf(format string, a ...interface{}) LogEntry {
	return LogEntry{Message: fmt.Sprintf(format, a...), Level: LevelDebug}
}
