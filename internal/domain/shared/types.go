package shared

import (
	"context"
	"time"
)

// Logger defines the interface for structured logging
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)
	With(fields ...Field) Logger
}

// Field represents a log field
type Field interface {
	Key() string
	Value() interface{}
}

// StringField creates a string field for logging
func StringField(key, value string) Field {
	return &field{key: key, value: value}
}

// IntField creates an int field for logging
func IntField(key string, value int) Field {
	return &field{key: key, value: value}
}

// ErrorField creates an error field for logging
func ErrorField(err error) Field {
	return &field{key: "error", value: err}
}

// DurationField creates a duration field for logging
func DurationField(key string, value time.Duration) Field {
	return &field{key: key, value: value}
}

type field struct {
	key   string
	value interface{}
}

func (f *field) Key() string {
	return f.key
}

func (f *field) Value() interface{} {
	return f.value
}

// Context keys for shared values
type ContextKey string

const (
	ContextKeyRequestID ContextKey = "request_id"
	ContextKeyUserID    ContextKey = "user_id"
	ContextKeyOperation ContextKey = "operation"
)

// GetRequestID extracts request ID from context
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(ContextKeyRequestID).(string); ok {
		return id
	}
	return ""
}

// WithRequestID adds request ID to context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, ContextKeyRequestID, requestID)
}

// GetOperation extracts operation name from context
func GetOperation(ctx context.Context) string {
	if op, ok := ctx.Value(ContextKeyOperation).(string); ok {
		return op
	}
	return ""
}

// WithOperation adds operation name to context
func WithOperation(ctx context.Context, operation string) context.Context {
	return context.WithValue(ctx, ContextKeyOperation, operation)
}

// Result represents a generic operation result
type Result[T any] struct {
	Value T
	Error error
}

// NewResult creates a new result
func NewResult[T any](value T, err error) Result[T] {
	return Result[T]{Value: value, Error: err}
}

// IsSuccess checks if the result is successful
func (r Result[T]) IsSuccess() bool {
	return r.Error == nil
}

// Unwrap returns the value and error
func (r Result[T]) Unwrap() (T, error) {
	return r.Value, r.Error
}

// Map transforms the result value if successful
func (r Result[T]) Map(fn func(T) interface{}) Result[interface{}] {
	if r.Error != nil {
		return Result[interface{}]{Error: r.Error}
	}
	return Result[interface{}]{Value: fn(r.Value)}
}

// Chain allows chaining operations on results
func (r Result[T]) Chain(fn func(T) error) Result[T] {
	if r.Error != nil {
		return r
	}
	return Result[T]{Value: r.Value, Error: fn(r.Value)}
}

// Validator defines an interface for domain object validation
type Validator interface {
	Validate() error
}

// Entity defines the base interface for domain entities
type Entity interface {
	Validator
	String() string
}

// ValueObject defines the base interface for value objects
type ValueObject interface {
	Validator
	Equal(other ValueObject) bool
}

// Repository defines the base interface for repositories
type Repository[T Entity] interface {
	Save(ctx context.Context, entity T) error
	FindByID(ctx context.Context, id string) (T, error)
	FindAll(ctx context.Context) ([]T, error)
	Delete(ctx context.Context, id string) error
}

// EventPublisher defines interface for domain event publishing
type EventPublisher interface {
	Publish(ctx context.Context, event DomainEvent) error
}

// DomainEvent represents a domain event
type DomainEvent interface {
	EventType() string
	OccurredAt() time.Time
	AggregateID() string
}

// BaseDomainEvent provides common domain event functionality
type BaseDomainEvent struct {
	Type        string    `json:"type"`
	Timestamp   time.Time `json:"occurred_at"`
	AggregateId string    `json:"aggregate_id"`
}

// EventType returns the event type
func (e BaseDomainEvent) EventType() string {
	return e.Type
}

// OccurredAt returns when the event occurred
func (e BaseDomainEvent) OccurredAt() time.Time {
	return e.Timestamp
}

// AggregateID returns the aggregate ID
func (e BaseDomainEvent) AggregateID() string {
	return e.AggregateId
}

// NewBaseDomainEvent creates a new base domain event
func NewBaseDomainEvent(eventType, aggregateID string) BaseDomainEvent {
	return BaseDomainEvent{
		Type:        eventType,
		Timestamp:   time.Now(),
		AggregateId: aggregateID,
	}
}
