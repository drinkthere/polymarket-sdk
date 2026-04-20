package errors

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

func (e *Error) Error() string { return e.Op + ": " + e.Message }

