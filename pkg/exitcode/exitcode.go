package exitcode

const (
	Success      = 0
	Error        = 1
	AuthRequired = 2
	NotFound     = 3
	Conflict     = 4
)

// String codes for JSON error responses (used with output.Error).
const (
	CodeError        = "ERROR"
	CodeAuthRequired = "AUTH_REQUIRED"
	CodeNotFound     = "NOT_FOUND"
	CodeConflict     = "CONFLICT"
)
