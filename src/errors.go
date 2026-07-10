package anchordtl

import (
	"errors"
	"fmt"
	"strings"
)

type ErrorCode string

const (
	CodeInvalid        ErrorCode = "invalid"
	CodeNotFound       ErrorCode = "not_found"
	CodeConflict       ErrorCode = "conflict"
	CodeInsufficient   ErrorCode = "insufficient"
	CodeUnauthorized   ErrorCode = "unauthorized"
	CodeState          ErrorCode = "state"
	CodeAssetMismatch  ErrorCode = "asset_mismatch"
	CodeInvariant      ErrorCode = "invariant"
	CodeAlreadyExists  ErrorCode = "already_exists"
	CodePolicyRejected ErrorCode = "policy_rejected"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrInsufficient = errors.New("insufficient balance")
	ErrInvalid      = errors.New("invalid input")
)

type ProtocolError struct {
	Code    ErrorCode
	Op      string
	Message string
	Cause   error
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return "<nil>"
	}
	var b strings.Builder
	if e.Op != "" {
		b.WriteString(e.Op)
		b.WriteString(": ")
	}
	if e.Code != "" {
		b.WriteString(string(e.Code))
	}
	if e.Message != "" {
		if b.Len() > 0 {
			b.WriteString(": ")
		}
		b.WriteString(e.Message)
	}
	if e.Cause != nil {
		if b.Len() > 0 {
			b.WriteString(": ")
		}
		b.WriteString(e.Cause.Error())
	}
	return b.String()
}

func (e *ProtocolError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func fail(code ErrorCode, op string, format string, args ...any) error {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	return &ProtocolError{Code: code, Op: op, Message: msg}
}

func wrap(code ErrorCode, op string, err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	return &ProtocolError{Code: code, Op: op, Message: msg, Cause: err}
}

func IsCode(err error, code ErrorCode) bool {
	var perr *ProtocolError
	if errors.As(err, &perr) {
		return perr.Code == code
	}
	switch code {
	case CodeNotFound:
		return errors.Is(err, ErrNotFound)
	case CodeInsufficient:
		return errors.Is(err, ErrInsufficient)
	case CodeInvalid:
		return errors.Is(err, ErrInvalid)
	default:
		return false
	}
}

func require(ok bool, code ErrorCode, op string, format string, args ...any) error {
	if ok {
		return nil
	}
	return fail(code, op, format, args...)
}
