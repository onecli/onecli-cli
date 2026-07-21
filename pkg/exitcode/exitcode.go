package exitcode

const (
	Success      = 0
	Error        = 1
	AuthRequired = 2
	NotFound     = 3
	Conflict     = 4
	Forbidden    = 5
)

// String codes for JSON error responses (used with output.Error).
const (
	CodeError        = "ERROR"
	CodeAuthRequired = "AUTH_REQUIRED"
	CodeNotFound     = "NOT_FOUND"
	CodeConflict     = "CONFLICT"
	CodeForbidden    = "FORBIDDEN"
	// CodeGone marks a retired endpoint (HTTP 410) — the action field names
	// the replacement. CodeValidation marks a server-rejected payload (422).
	// Both exit with the general Error code — the string code is the signal.
	CodeGone       = "GONE"
	CodeValidation = "VALIDATION_ERROR"
)
