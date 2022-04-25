package logger

import (
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

var Messages = make(chan []byte, 128)

const (
	ErrorLvl           = 0
	WarningLvl         = 1
	InfoLvl            = 2
	ActionLvl          = 3
	KeysLvl            = 4
	KeysNotAssignedLvl = 5
	AnalogLvl          = 6

	DebugLvl = 378
)

var (
	Error           = zap.Int("level", ErrorLvl)
	Warning         = zap.Int("level", WarningLvl)
	Info            = zap.Int("level", InfoLvl)
	Action          = zap.Int("level", ActionLvl)
	Keys            = zap.Int("level", KeysLvl)
	KeysNotAssigned = zap.Int("level", KeysNotAssignedLvl)
	Analog          = zap.Int("level", AnalogLvl)

	Debug = zap.Int("level", 378)
)

type chanWriter struct {
	sync.Mutex
	ws zapcore.WriteSyncer
}

func (w *chanWriter) Write(p []byte) (n int, err error) {
	w.Lock()
	var newSlice = make([]byte, len(p))
	copy(newSlice, p)
	// Messages <- []byte(strings.Replace(string(newSlice), "\n", "", -1))
	Messages <- newSlice
	w.Unlock()
	return len(p), nil
}

func (w *chanWriter) Sync() error {
	w.Lock()
	err := w.ws.Sync()
	w.Unlock()
	return err
}

type gkeJsonEncoder struct {
	zapcore.Encoder
}

func newGkeJsonEncoder(cfg zapcore.EncoderConfig) zapcore.Encoder {
	return gkeJsonEncoder{
		Encoder: zapcore.NewJSONEncoder(cfg),
	}
}

func (enc gkeJsonEncoder) Clone() zapcore.Encoder {
	return gkeJsonEncoder{
		Encoder: enc.Encoder.Clone(),
	}
}

func (enc gkeJsonEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	fields = append(fields,
		zap.Object("logging.googleapis.com/sourceLocation", entryCaller{EntryCaller: &ent.Caller}),
	)
	return enc.Encoder.EncodeEntry(ent, fields)
}

type entryCaller struct {
	*zapcore.EntryCaller
}

func (c entryCaller) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("function", c.EntryCaller.Function)
	enc.AddString("file", c.EntryCaller.TrimmedPath())
	enc.AddInt("line", c.EntryCaller.Line)
	return nil
}

func GetLogger() *zap.Logger {
	writer := &chanWriter{}
	cfg := zap.NewProductionEncoderConfig()
	// // cfg := zap.NewDevelopmentEncoderConfig()
	cfg.SkipLineEnding = true
	cfg.EncodeTime = zapcore.EpochNanosTimeEncoder
	cfg.LevelKey = ""
	encoder := zapcore.NewJSONEncoder(cfg)
	noSync := zapcore.Lock(writer)
	// core := zapcore.NewCore(encoder, noSync, zap.DebugLevel)
	// return zap.New(core)

	// encoderConfig := zapcore.EncoderConfig{
	// 	TimeKey:       "time",
	// 	LevelKey:      "severity",
	// 	NameKey:       "logger",
	// 	MessageKey:    "message",
	// 	StacktraceKey: "stacktrace",
	// 	EncodeLevel:   zapcore.LowercaseLevelEncoder,
	// 	EncodeTime:    zapcore.RFC3339NanoTimeEncoder,
	//
	// 	SkipLineEnding: true,
	// }
	// encoder := newGkeJsonEncoder(encoderConfig)
	logger := zap.New(
		zapcore.NewCore(encoder, noSync, zap.DebugLevel),
		zap.AddCaller(),
	)

	return logger
}
