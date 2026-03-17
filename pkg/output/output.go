package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Writer handles all structured output for the CLI.
// All stdout/stderr writing must go through this. Never use fmt.Print or os.Stdout directly.
type Writer struct {
	out io.Writer
	err io.Writer
}

// New creates a Writer that writes to stdout and stderr.
func New() *Writer {
	return &Writer{
		out: os.Stdout,
		err: os.Stderr,
	}
}

// NewWithWriters creates a Writer with custom destinations (useful for testing).
func NewWithWriters(out, err io.Writer) *Writer {
	return &Writer{
		out: out,
		err: err,
	}
}

// Write marshals v as indented JSON and writes it to stdout.
// HTML escaping is disabled because this is a CLI tool, not a web page.
func (w *Writer) Write(v any) error {
	data, err := marshalIndent(v)
	if err != nil {
		return fmt.Errorf("marshaling output: %w", err)
	}
	_, writeErr := w.out.Write(data)
	if writeErr != nil {
		return fmt.Errorf("writing output: %w", writeErr)
	}
	return nil
}

// WriteFiltered marshals v as JSON, then filters to only include the specified
// fields (comma-separated). If fields is empty, it behaves like Write.
// Works on top-level object keys and on arrays of objects.
func (w *Writer) WriteFiltered(v any, fields string) error {
	if fields == "" {
		return w.Write(v)
	}

	data, err := marshalIndent(v)
	if err != nil {
		return fmt.Errorf("marshaling output: %w", err)
	}

	allowed := parseFields(fields)
	filtered, err := filterJSON(data, allowed)
	if err != nil {
		return fmt.Errorf("filtering fields: %w", err)
	}

	_, writeErr := w.out.Write(filtered)
	if writeErr != nil {
		return fmt.Errorf("writing output: %w", writeErr)
	}
	return nil
}

// WriteQuiet extracts a single field from v and writes just the raw value
// (no JSON wrapping), one per line for arrays. Enables piping.
func (w *Writer) WriteQuiet(v any, field string) error {
	data, err := marshalIndent(v)
	if err != nil {
		return fmt.Errorf("marshaling output: %w", err)
	}

	// Try as array first.
	var arr []json.RawMessage
	if json.Unmarshal(data, &arr) == nil {
		for _, item := range arr {
			val, extractErr := extractField(item, field)
			if extractErr != nil {
				continue
			}
			fmt.Fprintln(w.out, val)
		}
		return nil
	}

	// Try as object with a "data" wrapper (common in list responses).
	var wrapper map[string]json.RawMessage
	if json.Unmarshal(data, &wrapper) == nil {
		if dataField, ok := wrapper["data"]; ok {
			if json.Unmarshal(dataField, &arr) == nil {
				for _, item := range arr {
					val, extractErr := extractField(item, field)
					if extractErr != nil {
						continue
					}
					fmt.Fprintln(w.out, val)
				}
				return nil
			}
		}
	}

	// Single object.
	val, err := extractField(data, field)
	if err != nil {
		return fmt.Errorf("extracting field %q: %w", field, err)
	}
	fmt.Fprintln(w.out, val)
	return nil
}

// DryRunResponse is the JSON shape for --dry-run output.
type DryRunResponse struct {
	DryRun      bool   `json:"dry_run"`
	Description string `json:"description"`
	Payload     any    `json:"payload"`
}

// WriteDryRun outputs a dry-run response showing what would happen without
// actually executing the operation.
func (w *Writer) WriteDryRun(description string, payload any) error {
	return w.Write(DryRunResponse{
		DryRun:      true,
		Description: description,
		Payload:     payload,
	})
}

// parseFields splits a comma-separated field list into a set.
func parseFields(fields string) map[string]bool {
	set := make(map[string]bool)
	for _, f := range strings.Split(fields, ",") {
		f = strings.TrimSpace(f)
		if f != "" {
			set[f] = true
		}
	}
	return set
}

// filterJSON filters top-level keys of a JSON object or each element of an array.
func filterJSON(data []byte, allowed map[string]bool) ([]byte, error) {
	// Try array first.
	var arr []json.RawMessage
	if json.Unmarshal(data, &arr) == nil {
		var result []json.RawMessage
		for _, item := range arr {
			filtered, err := filterObject(item, allowed)
			if err != nil {
				return nil, err
			}
			result = append(result, filtered)
		}
		return marshalIndent(result)
	}

	// Try as object with "data" wrapper.
	var wrapper map[string]json.RawMessage
	if json.Unmarshal(data, &wrapper) == nil {
		if dataField, ok := wrapper["data"]; ok {
			if json.Unmarshal(dataField, &arr) == nil {
				var result []json.RawMessage
				for _, item := range arr {
					filtered, err := filterObject(item, allowed)
					if err != nil {
						return nil, err
					}
					result = append(result, filtered)
				}
				filteredData, err := json.Marshal(result)
				if err != nil {
					return nil, err
				}
				wrapper["data"] = filteredData
				return marshalIndent(wrapper)
			}
		}
	}

	// Single object.
	return filterObject(data, allowed)
}

// filterObject keeps only allowed keys from a JSON object.
func filterObject(data json.RawMessage, allowed map[string]bool) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return data, nil // not an object, return as-is
	}

	filtered := make(map[string]json.RawMessage)
	for k, v := range obj {
		if allowed[k] {
			filtered[k] = v
		}
	}

	return json.Marshal(filtered)
}

// extractField gets the value of a single field from a JSON object, returning
// it as a bare string (unquoted for strings, raw for other types).
func extractField(data []byte, field string) (string, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return "", err
	}

	raw, ok := obj[field]
	if !ok {
		return "", fmt.Errorf("field %q not found", field)
	}

	// Unquote if it's a JSON string.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}

	return string(raw), nil
}

// ErrorResponse is the JSON shape written to stderr on errors.
type ErrorResponse struct {
	Error  string `json:"error"`
	Code   string `json:"code"`
	Action string `json:"action,omitempty"`
}

// Stderr writes a plain text line to stderr. Use only for interactive flows
// where human-readable output is appropriate.
func (w *Writer) Stderr(msg string) {
	fmt.Fprintln(w.err, msg)
}

// Error writes a structured JSON error to stderr.
func (w *Writer) Error(code string, msg string) error {
	return w.writeError(ErrorResponse{
		Error: msg,
		Code:  code,
	})
}

// ErrorWithAction writes a structured JSON error to stderr with an action hint
// telling the agent what command to run next.
func (w *Writer) ErrorWithAction(code string, msg string, action string) error {
	return w.writeError(ErrorResponse{
		Error:  msg,
		Code:   code,
		Action: action,
	})
}

func (w *Writer) writeError(resp ErrorResponse) error {
	data, err := marshalIndent(resp)
	if err != nil {
		return fmt.Errorf("marshaling error output: %w", err)
	}
	_, writeErr := w.err.Write(data)
	if writeErr != nil {
		return fmt.Errorf("writing error output: %w", writeErr)
	}
	return nil
}

// marshalIndent encodes v as indented JSON with HTML escaping disabled.
// Go's json.Marshal escapes &, <, > as unicode sequences (\u0026 etc.)
// which breaks URLs in CLI output. Agents and humans both need raw characters.
func marshalIndent(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
