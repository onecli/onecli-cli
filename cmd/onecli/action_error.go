package main

import (
	"errors"

	"github.com/onecli/onecli-cli/pkg/output"
)

// actionError is a command error that carries its own string code, action
// hint and exit code, for when the generic status mapping in handleError
// would give the wrong next step.
type actionError struct {
	code   string
	msg    string
	action string
	exit   int
}

func (e *actionError) Error() string { return e.msg }

// writeActionError prints err when it is an actionError and returns its
// exit code. ok is false for any other error.
func writeActionError(out *output.Writer, err error) (exit int, ok bool) {
	var ae *actionError
	if !errors.As(err, &ae) {
		return 0, false
	}
	if ae.action != "" {
		_ = out.ErrorWithAction(ae.code, ae.msg, ae.action)
	} else {
		_ = out.Error(ae.code, ae.msg)
	}
	return ae.exit, true
}
