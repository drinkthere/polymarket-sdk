package errors

import (
	"strconv"
	"strings"
)

type ErrorKind string

const (
	ErrRequestBuild ErrorKind = "request_build"
	ErrAuth         ErrorKind = "auth"
	ErrTimeout      ErrorKind = "timeout"
	ErrNetwork      ErrorKind = "network"
	ErrAPI          ErrorKind = "api"
	ErrDecode       ErrorKind = "decode"
	ErrProtocol     ErrorKind = "protocol"
	ErrClosed       ErrorKind = "closed"
)

type Error struct {
	Kind       ErrorKind
	Op         string
	Method     string
	URL        string
	StatusCode int
	Code       string
	Message    string
	RawBody    []byte
	Cause      error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	var b strings.Builder
	if e.Op != "" {
		b.WriteString(e.Op)
	} else {
		b.WriteString("polymarket-sdk")
	}

	if e.Kind != "" {
		b.WriteString(" ")
		b.WriteString(string(e.Kind))
	}

	if e.Method != "" {
		b.WriteString(" ")
		b.WriteString(e.Method)
	}
	if e.URL != "" {
		b.WriteString(" ")
		b.WriteString(e.URL)
	}
	if e.StatusCode != 0 {
		b.WriteString(" status=")
		b.WriteString(strconv.Itoa(e.StatusCode))
	}
	if e.Code != "" {
		b.WriteString(" code=")
		b.WriteString(e.Code)
	}

	msg := strings.TrimSpace(e.Message)
	if msg == "" && e.Cause != nil {
		msg = strings.TrimSpace(e.Cause.Error())
	}
	if msg != "" {
		b.WriteString(": ")
		b.WriteString(msg)
	}
	return b.String()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
