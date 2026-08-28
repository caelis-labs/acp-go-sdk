package acp

import (
	"errors"
	"fmt"
)

// ErrTransportFailure classifies failures while reading from or writing to
// the connection transport. Use errors.As with *TransportError to inspect the
// operation and underlying cause.
var ErrTransportFailure = errors.New("acp: transport failure")

// TransportOperation identifies the transport operation that failed.
type TransportOperation string

const (
	TransportOperationRead  TransportOperation = "read"
	TransportOperationWrite TransportOperation = "write"
)

// TransportError reports a connection transport failure without discarding
// the underlying error. Prepared requests may wrap this error with an
// independent RequestSubmissionState classification.
type TransportError struct {
	Op    TransportOperation
	Cause error
}

func (e *TransportError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause == nil {
		return fmt.Sprintf("acp: transport %s failed", e.Op)
	}
	return fmt.Sprintf("acp: transport %s failed: %v", e.Op, e.Cause)
}

func (e *TransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *TransportError) Is(target error) bool {
	return target == ErrTransportFailure
}

func transportError(operation TransportOperation, cause error) error {
	if cause == nil {
		return nil
	}
	return &TransportError{Op: operation, Cause: cause}
}
