package logging

import (
	"fmt"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/italoag/ghcloner/internal/domain/shared"
)

// LogEntry represents a single log entry for TUI display
type LogEntry struct {
	Timestamp time.Time         `json:"timestamp"`
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Fields    map[string]interface{} `json:"fields"`
}

// String returns a formatted string representation of the log entry
func (e *LogEntry) String() string {
	return fmt.Sprintf("[%s] %s %s", e.Level, e.Timestamp.Format("15:04:05"), e.Message)
}

// LogBuffer manages a circular buffer of log entries for TUI display
type LogBuffer struct {
	entries []LogEntry
	size    int
	current int
	mutex   sync.RWMutex
	notify  chan struct{}
}

// NewLogBuffer creates a new log buffer with specified size
func NewLogBuffer(size int) *LogBuffer {
	if size <= 0 {
		size = 100 // Default size
	}
	return &LogBuffer{
		entries: make([]LogEntry, size),
		size:    size,
		notify:  make(chan struct{}, 1),
	}
}

// Add adds a new log entry to the buffer
func (lb *LogBuffer) Add(entry LogEntry) {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()
	
	lb.entries[lb.current] = entry
	lb.current = (lb.current + 1) % lb.size
	
	// Notify listeners of new entry
	select {
	case lb.notify <- struct{}{}:
	default:
	}
}

// GetRecent returns the most recent log entries (up to limit)
func (lb *LogBuffer) GetRecent(limit int) []LogEntry {
	lb.mutex.RLock()
	defer lb.mutex.RUnlock()
	
	if limit <= 0 || limit > lb.size {
		limit = lb.size
	}
	
	var result []LogEntry
	
	// Start from the oldest entry and collect up to limit entries
	for i := 0; i < limit; i++ {
		idx := (lb.current - limit + i + lb.size) % lb.size
		entry := lb.entries[idx]
		
		// Skip empty entries (buffer not yet full)
		if !entry.Timestamp.IsZero() {
			result = append(result, entry)
		}
	}
	
	return result
}

// GetNotifyChannel returns a channel that receives notifications when new logs are added
func (lb *LogBuffer) GetNotifyChannel() <-chan struct{} {
	return lb.notify
}

// Clear clears all log entries
func (lb *LogBuffer) Clear() {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()
	
	lb.entries = make([]LogEntry, lb.size)
	lb.current = 0
}

// ZapLogger implements the shared.Logger interface using zap
type ZapLogger struct {
	logger *zap.Logger
}

// LoggerConfig holds configuration for the logger
type LoggerConfig struct {
	Level       string // debug, info, warn, error
	Encoding    string // json, console
	OutputPaths []string
	Development bool
}

// NewZapLogger creates a new zap-based logger
func NewZapLogger(config *LoggerConfig) (*ZapLogger, error) {
	if config == nil {
		config = &LoggerConfig{
			Level:       "info",
			Encoding:    "console",
			OutputPaths: []string{"stdout"},
			Development: false,
		}
	}

	// Parse log level
	level, err := zapcore.ParseLevel(config.Level)
	if err != nil {
		return nil, fmt.Errorf("invalid log level: %w", err)
	}

	// Create encoder config
	var encoderConfig zapcore.EncoderConfig
	if config.Development {
		encoderConfig = zap.NewDevelopmentEncoderConfig()
		encoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05.000")
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		encoderConfig = zap.NewProductionEncoderConfig()
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}

	// Create encoder
	var encoder zapcore.Encoder
	switch config.Encoding {
	case "json":
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	case "console":
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	default:
		return nil, fmt.Errorf("invalid encoding: %s", config.Encoding)
	}

	// Create writers
	var writers []zapcore.WriteSyncer
	for _, path := range config.OutputPaths {
		if path == "stdout" {
			writers = append(writers, zapcore.AddSync(os.Stdout))
		} else if path == "stderr" {
			writers = append(writers, zapcore.AddSync(os.Stderr))
		} else {
			file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
			if err != nil {
				return nil, fmt.Errorf("failed to open log file %s: %w", path, err)
			}
			writers = append(writers, zapcore.AddSync(file))
		}
	}

	// Combine writers
	writer := zapcore.NewMultiWriteSyncer(writers...)

	// Create core
	core := zapcore.NewCore(encoder, writer, level)

	// Create logger
	var logger *zap.Logger
	if config.Development {
		logger = zap.New(core, zap.Development(), zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
	} else {
		logger = zap.New(core, zap.AddCaller())
	}

	return &ZapLogger{logger: logger}, nil
}

// Debug logs a debug message
func (l *ZapLogger) Debug(msg string, fields ...shared.Field) {
	zapFields := l.convertFields(fields)
	l.logger.Debug(msg, zapFields...)
}

// Info logs an info message
func (l *ZapLogger) Info(msg string, fields ...shared.Field) {
	zapFields := l.convertFields(fields)
	l.logger.Info(msg, zapFields...)
}

// Warn logs a warning message
func (l *ZapLogger) Warn(msg string, fields ...shared.Field) {
	zapFields := l.convertFields(fields)
	l.logger.Warn(msg, zapFields...)
}

// Error logs an error message
func (l *ZapLogger) Error(msg string, fields ...shared.Field) {
	zapFields := l.convertFields(fields)
	l.logger.Error(msg, zapFields...)
}

// Fatal logs a fatal message and exits
func (l *ZapLogger) Fatal(msg string, fields ...shared.Field) {
	zapFields := l.convertFields(fields)
	l.logger.Fatal(msg, zapFields...)
}

// With creates a new logger with additional fields
func (l *ZapLogger) With(fields ...shared.Field) shared.Logger {
	zapFields := l.convertFields(fields)
	return &ZapLogger{logger: l.logger.With(zapFields...)}
}

// convertFields converts shared.Field to zap.Field
func (l *ZapLogger) convertFields(fields []shared.Field) []zap.Field {
	zapFields := make([]zap.Field, len(fields))
	for i, field := range fields {
		zapFields[i] = l.convertField(field)
	}
	return zapFields
}

// convertField converts a single shared.Field to zap.Field
func (l *ZapLogger) convertField(field shared.Field) zap.Field {
	key := field.Key()
	value := field.Value()

	switch v := value.(type) {
	case string:
		return zap.String(key, v)
	case int:
		return zap.Int(key, v)
	case int64:
		return zap.Int64(key, v)
	case float64:
		return zap.Float64(key, v)
	case bool:
		return zap.Bool(key, v)
	case time.Duration:
		return zap.Duration(key, v)
	case time.Time:
		return zap.Time(key, v)
	case error:
		return zap.Error(v)
	default:
		return zap.Any(key, v)
	}
}

// Sync flushes any buffered log entries
func (l *ZapLogger) Sync() error {
	return l.logger.Sync()
}

// Close closes the logger
func (l *ZapLogger) Close() error {
	return l.logger.Sync()
}

// NoOpLogger implements a no-operation logger for testing
type NoOpLogger struct{}

// NewNoOpLogger creates a new no-op logger
func NewNoOpLogger() *NoOpLogger {
	return &NoOpLogger{}
}

// Debug does nothing
func (l *NoOpLogger) Debug(msg string, fields ...shared.Field) {}

// Info does nothing
func (l *NoOpLogger) Info(msg string, fields ...shared.Field) {}

// Warn does nothing
func (l *NoOpLogger) Warn(msg string, fields ...shared.Field) {}

// Error does nothing
func (l *NoOpLogger) Error(msg string, fields ...shared.Field) {}

// Fatal does nothing
func (l *NoOpLogger) Fatal(msg string, fields ...shared.Field) {}

// With returns the same logger
func (l *NoOpLogger) With(fields ...shared.Field) shared.Logger {
	return l
}

// FileLogger creates a logger that writes to a file
func NewFileLogger(filename string, level string) (*ZapLogger, error) {
	config := &LoggerConfig{
		Level:       level,
		Encoding:    "json",
		OutputPaths: []string{filename},
		Development: false,
	}
	return NewZapLogger(config)
}

// ConsoleLogger creates a logger that writes to console
func NewConsoleLogger(level string, development bool) (*ZapLogger, error) {
	encoding := "console"
	if !development {
		encoding = "json"
	}

	config := &LoggerConfig{
		Level:       level,
		Encoding:    encoding,
		OutputPaths: []string{"stdout"},
		Development: development,
	}
	return NewZapLogger(config)
}

// TUILogger provides logging for TUI applications with file output and log buffering
type TUILogger struct {
	fileLogger *ZapLogger
	buffer     *LogBuffer
	logFile    string
}

// TUILoggerConfig holds configuration for TUI logger
type TUILoggerConfig struct {
	LogFile     string // File path for persistent logging
	Level       string // Log level (debug, info, warn, error)
	BufferSize  int    // Size of the log buffer for TUI display
	Development bool   // Development mode
}

// NewTUILogger creates a new TUI-compatible logger
func NewTUILogger(config *TUILoggerConfig) (*TUILogger, error) {
	if config == nil {
		config = &TUILoggerConfig{
			LogFile:     "ghclone.log",
			Level:       "info",
			BufferSize:  50,
			Development: true,
		}
	}

	// Ensure log file directory exists
	if err := ensureLogFileDir(config.LogFile); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Create file logger
	fileLoggerConfig := &LoggerConfig{
		Level:       config.Level,
		Encoding:    "json",
		OutputPaths: []string{config.LogFile},
		Development: config.Development,
	}

	fileLogger, err := NewZapLogger(fileLoggerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create file logger: %w", err)
	}

	// Create log buffer
	buffer := NewLogBuffer(config.BufferSize)

	return &TUILogger{
		fileLogger: fileLogger,
		buffer:     buffer,
		logFile:    config.LogFile,
	}, nil
}

// ensureLogFileDir creates the directory for the log file if it doesn't exist
func ensureLogFileDir(logFile string) error {
	dir := ""
	for i := len(logFile) - 1; i >= 0; i-- {
		if logFile[i] == '/' || logFile[i] == '\\' {
			dir = logFile[:i]
			break
		}
	}
	
	if dir != "" {
		return os.MkdirAll(dir, 0755)
	}
	return nil
}

// Debug logs a debug message
func (tl *TUILogger) Debug(msg string, fields ...shared.Field) {
	tl.fileLogger.Debug(msg, fields...)
	tl.addToBuffer("DEBUG", msg, fields)
}

// Info logs an info message
func (tl *TUILogger) Info(msg string, fields ...shared.Field) {
	tl.fileLogger.Info(msg, fields...)
	tl.addToBuffer("INFO", msg, fields)
}

// Warn logs a warning message
func (tl *TUILogger) Warn(msg string, fields ...shared.Field) {
	tl.fileLogger.Warn(msg, fields...)
	tl.addToBuffer("WARN", msg, fields)
}

// Error logs an error message
func (tl *TUILogger) Error(msg string, fields ...shared.Field) {
	tl.fileLogger.Error(msg, fields...)
	tl.addToBuffer("ERROR", msg, fields)
}

// Fatal logs a fatal message
func (tl *TUILogger) Fatal(msg string, fields ...shared.Field) {
	tl.fileLogger.Fatal(msg, fields...)
	tl.addToBuffer("FATAL", msg, fields)
}

// With creates a new logger with additional fields
func (tl *TUILogger) With(fields ...shared.Field) shared.Logger {
	return &TUILogger{
		fileLogger: tl.fileLogger.With(fields...).(*ZapLogger),
		buffer:     tl.buffer, // Share the same buffer
		logFile:    tl.logFile,
	}
}

// addToBuffer adds a log entry to the buffer for TUI display
func (tl *TUILogger) addToBuffer(level, msg string, fields []shared.Field) {
	fieldsMap := make(map[string]interface{})
	for _, field := range fields {
		fieldsMap[field.Key()] = field.Value()
	}

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   msg,
		Fields:    fieldsMap,
	}

	tl.buffer.Add(entry)
}

// GetLogBuffer returns the log buffer for TUI access
func (tl *TUILogger) GetLogBuffer() *LogBuffer {
	return tl.buffer
}

// GetLogFile returns the path to the log file
func (tl *TUILogger) GetLogFile() string {
	return tl.logFile
}

// Close closes the logger and flushes any buffered data
func (tl *TUILogger) Close() error {
	return tl.fileLogger.Close()
}

// MultiOutputLogger creates a logger that writes to multiple outputs
func NewMultiOutputLogger(level string, development bool, outputs ...string) (*ZapLogger, error) {
	encoding := "console"
	if !development {
		encoding = "json"
	}

	config := &LoggerConfig{
		Level:       level,
		Encoding:    encoding,
		OutputPaths: outputs,
		Development: development,
	}
	return NewZapLogger(config)
}

// ContextualLogger wraps a logger with additional context fields
type ContextualLogger struct {
	logger shared.Logger
	fields []shared.Field
}

// NewContextualLogger creates a new contextual logger
func NewContextualLogger(logger shared.Logger, fields ...shared.Field) *ContextualLogger {
	return &ContextualLogger{
		logger: logger,
		fields: fields,
	}
}

// Debug logs a debug message with context
func (l *ContextualLogger) Debug(msg string, fields ...shared.Field) {
	allFields := append(l.fields, fields...)
	l.logger.Debug(msg, allFields...)
}

// Info logs an info message with context
func (l *ContextualLogger) Info(msg string, fields ...shared.Field) {
	allFields := append(l.fields, fields...)
	l.logger.Info(msg, allFields...)
}

// Warn logs a warning message with context
func (l *ContextualLogger) Warn(msg string, fields ...shared.Field) {
	allFields := append(l.fields, fields...)
	l.logger.Warn(msg, allFields...)
}

// Error logs an error message with context
func (l *ContextualLogger) Error(msg string, fields ...shared.Field) {
	allFields := append(l.fields, fields...)
	l.logger.Error(msg, allFields...)
}

// Fatal logs a fatal message with context
func (l *ContextualLogger) Fatal(msg string, fields ...shared.Field) {
	allFields := append(l.fields, fields...)
	l.logger.Fatal(msg, allFields...)
}

// With creates a new logger with additional fields
func (l *ContextualLogger) With(fields ...shared.Field) shared.Logger {
	allFields := append(l.fields, fields...)
	return &ContextualLogger{
		logger: l.logger,
		fields: allFields,
	}
}

// LoggerManager manages multiple loggers and log rotation
type LoggerManager struct {
	loggers map[string]shared.Logger
	config  *LoggerConfig
}

// NewLoggerManager creates a new logger manager
func NewLoggerManager(config *LoggerConfig) *LoggerManager {
	return &LoggerManager{
		loggers: make(map[string]shared.Logger),
		config:  config,
	}
}

// GetLogger gets or creates a logger with the given name
func (lm *LoggerManager) GetLogger(name string) (shared.Logger, error) {
	if logger, exists := lm.loggers[name]; exists {
		return logger, nil
	}

	// Create a new logger with name prefix
	config := *lm.config // Copy config
	logger, err := NewZapLogger(&config)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger %s: %w", name, err)
	}

	// Add name as context
	contextualLogger := NewContextualLogger(logger, shared.StringField("logger", name))
	lm.loggers[name] = contextualLogger

	return contextualLogger, nil
}

// SetLogLevel updates the log level for all loggers
func (lm *LoggerManager) SetLogLevel(level string) error {
	// Validate level
	if _, err := zapcore.ParseLevel(level); err != nil {
		return fmt.Errorf("invalid log level: %w", err)
	}

	lm.config.Level = level

	// Clear existing loggers to force recreation with new level
	lm.loggers = make(map[string]shared.Logger)

	return nil
}

// Close closes all managed loggers
func (lm *LoggerManager) Close() error {
	for name, logger := range lm.loggers {
		if zapLogger, ok := logger.(*ContextualLogger); ok {
			if zapImpl, ok := zapLogger.logger.(*ZapLogger); ok {
				if err := zapImpl.Close(); err != nil {
					return fmt.Errorf("failed to close logger %s: %w", name, err)
				}
			}
		}
	}
	return nil
}