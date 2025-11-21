package logger

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Logger struct {
	logger zerolog.Logger
}

func New(level, format string) *Logger {
	var output io.Writer = os.Stdout

	if format == "pretty" {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	}

	zLevel, err := zerolog.ParseLevel(level)
	if err != nil {
		zLevel = zerolog.InfoLevel
	}

	zerolog.SetGlobalLevel(zLevel)
	logger := zerolog.New(output).With().Timestamp().Caller().Logger()

	log.Logger = logger

	return &Logger{logger: logger}
}

func (l *Logger) Info() *zerolog.Event {
	return l.logger.Info()
}

func (l *Logger) Error() *zerolog.Event {
	return l.logger.Error()
}

func (l *Logger) Debug() *zerolog.Event {
	return l.logger.Debug()
}

func (l *Logger) Warn() *zerolog.Event {
	return l.logger.Warn()
}

func (l *Logger) Fatal() *zerolog.Event {
	return l.logger.Fatal()
}

func (l *Logger) With() zerolog.Context {
	return l.logger.With()
}

func (l *Logger) GetZerolog() zerolog.Logger {
	return l.logger
}
