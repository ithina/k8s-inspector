package dify

import "fmt"

// Error types for stream parsing
type ErrorType int

const (
	ErrIncompleteResponse ErrorType = iota
	ErrEmptyResponse
	ErrInvalidContent
	ErrStreamTimeout
	ErrParseFailure
)

// StreamError represents a stream parsing error
type StreamError struct {
	Type    ErrorType
	Message string
	Cause   error
}

func (e *StreamError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func NewStreamError(errType ErrorType, msg string, cause error) *StreamError {
	return &StreamError{
		Type:    errType,
		Message: msg,
		Cause:   cause,
	}
}
