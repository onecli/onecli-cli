package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWrite(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})

	type item struct {
		Name string `json:"name"`
		ID   int    `json:"id"`
	}

	if err := w.Write(item{Name: "test", ID: 42}); err != nil {
		t.Fatal(err)
	}

	var got item
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, out.String())
	}
	if got.Name != "test" || got.ID != 42 {
		t.Errorf("got %+v, want {Name:test ID:42}", got)
	}
}

func TestWriteHTMLNotEscaped(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})

	if err := w.Write(map[string]string{"url": "https://example.com/a&b=1"}); err != nil {
		t.Fatal(err)
	}

	// Go's default json.Marshal escapes & to \u0026. We must not.
	if strings.Contains(out.String(), `\u0026`) {
		t.Errorf("URL was HTML-escaped: %s", out.String())
	}
	if !strings.Contains(out.String(), `&`) {
		t.Errorf("expected raw & in output: %s", out.String())
	}
}

func TestWriteFiltered(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})

	data := map[string]string{"id": "abc", "name": "test", "extra": "drop"}

	if err := w.WriteFiltered(data, "id,name"); err != nil {
		t.Fatal(err)
	}

	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if got["id"] != "abc" || got["name"] != "test" {
		t.Errorf("expected id+name, got %v", got)
	}
	if _, ok := got["extra"]; ok {
		t.Error("extra field should have been filtered out")
	}
}

func TestWriteFilteredEmptyPassthrough(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})

	data := map[string]string{"id": "abc", "name": "test"}
	if err := w.WriteFiltered(data, ""); err != nil {
		t.Fatal(err)
	}

	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("empty fields should pass through all keys, got %v", got)
	}
}

func TestWriteFilteredArray(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})

	data := []map[string]string{
		{"id": "a", "name": "one", "extra": "x"},
		{"id": "b", "name": "two", "extra": "y"},
	}

	if err := w.WriteFiltered(data, "id"); err != nil {
		t.Fatal(err)
	}

	var got []map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}
	for _, item := range got {
		if _, ok := item["name"]; ok {
			t.Error("name should have been filtered out")
		}
		if _, ok := item["extra"]; ok {
			t.Error("extra should have been filtered out")
		}
	}
}

func TestWriteQuietSingleObject(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})

	if err := w.WriteQuiet(map[string]string{"id": "abc123", "name": "test"}, "id"); err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(out.String())
	if got != "abc123" {
		t.Errorf("got %q, want %q", got, "abc123")
	}
}

func TestWriteQuietArray(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})

	data := []map[string]string{
		{"id": "a"},
		{"id": "b"},
		{"id": "c"},
	}

	if err := w.WriteQuiet(data, "id"); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "a" || lines[1] != "b" || lines[2] != "c" {
		t.Errorf("got %v, want [a b c]", lines)
	}
}

func TestWriteQuietNumericField(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})

	if err := w.WriteQuiet(map[string]any{"count": 42}, "count"); err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(out.String())
	if got != "42" {
		t.Errorf("got %q, want %q", got, "42")
	}
}

func TestWriteDryRun(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})

	if err := w.WriteDryRun("Would create agent", map[string]string{"name": "test"}); err != nil {
		t.Fatal(err)
	}

	var got DryRunResponse
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if !got.DryRun {
		t.Error("dry_run should be true")
	}
	if got.Description != "Would create agent" {
		t.Errorf("description = %q", got.Description)
	}
}

func TestError(t *testing.T) {
	var stderr bytes.Buffer
	w := NewWithWriters(&bytes.Buffer{}, &stderr)

	if err := w.Error("ERROR", "something went wrong"); err != nil {
		t.Fatal(err)
	}

	var got ErrorResponse
	if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
		t.Fatalf("stderr is not valid JSON: %v\nraw: %s", err, stderr.String())
	}
	if got.Error != "something went wrong" {
		t.Errorf("error = %q", got.Error)
	}
	if got.Code != "ERROR" {
		t.Errorf("code = %q", got.Code)
	}
	if got.Action != "" {
		t.Errorf("action should be empty, got %q", got.Action)
	}
}

func TestErrorWithAction(t *testing.T) {
	var stderr bytes.Buffer
	w := NewWithWriters(&bytes.Buffer{}, &stderr)

	if err := w.ErrorWithAction("AUTH_REQUIRED", "Unauthorized", "onecli auth login"); err != nil {
		t.Fatal(err)
	}

	var got ErrorResponse
	if err := json.Unmarshal(stderr.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Code != "AUTH_REQUIRED" {
		t.Errorf("code = %q", got.Code)
	}
	if got.Action != "onecli auth login" {
		t.Errorf("action = %q", got.Action)
	}
}

func TestWriteWithHint(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})
	w.SetHint("Manage your agents \u2192 https://app.onecli.sh")

	if err := w.Write(map[string]string{"id": "abc", "status": "ok"}); err != nil {
		t.Fatal(err)
	}

	raw := out.String()

	// hint must be the first key
	hintIdx := strings.Index(raw, `"hint"`)
	idIdx := strings.Index(raw, `"id"`)
	if hintIdx < 0 || idIdx < 0 || hintIdx >= idIdx {
		t.Errorf("hint must appear before other keys:\n%s", raw)
	}

	var got map[string]string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, raw)
	}
	if got["hint"] != "Manage your agents \u2192 https://app.onecli.sh" {
		t.Errorf("hint = %q", got["hint"])
	}
	if got["id"] != "abc" {
		t.Errorf("id = %q", got["id"])
	}
}

func TestWriteWithHintArray(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})
	w.SetHint("Manage your secrets \u2192 https://app.onecli.sh")

	data := []map[string]string{{"id": "a"}, {"id": "b"}}
	if err := w.Write(data); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Hint string              `json:"hint"`
		Data []map[string]string `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if got.Hint != "Manage your secrets \u2192 https://app.onecli.sh" {
		t.Errorf("hint = %q", got.Hint)
	}
	if len(got.Data) != 2 {
		t.Errorf("expected 2 items, got %d", len(got.Data))
	}
}

func TestWriteNoHintWhenEmpty(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})

	if err := w.Write(map[string]string{"id": "abc"}); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out.String(), "hint") {
		t.Errorf("should not contain hint when not set:\n%s", out.String())
	}
}

func TestWriteQuietNoHint(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})
	w.SetHint("Manage your agents \u2192 https://app.onecli.sh")

	if err := w.WriteQuiet(map[string]string{"id": "abc"}, "id"); err != nil {
		t.Fatal(err)
	}

	got := strings.TrimSpace(out.String())
	if got != "abc" {
		t.Errorf("got %q, want %q", got, "abc")
	}
}

func TestWriteFilteredWithHintExcluded(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})
	w.SetHint("Manage your secrets \u2192 https://app.onecli.sh")

	data := map[string]string{"id": "abc", "name": "test", "extra": "drop"}
	if err := w.WriteFiltered(data, "id,name"); err != nil {
		t.Fatal(err)
	}

	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if _, ok := got["hint"]; ok {
		t.Error("hint should not be present when --fields is specified")
	}
	if got["id"] != "abc" {
		t.Errorf("id = %q", got["id"])
	}
	if _, ok := got["extra"]; ok {
		t.Error("extra should have been filtered out")
	}
}

func TestWriteFilteredEmptyFieldsWithHint(t *testing.T) {
	var out bytes.Buffer
	w := NewWithWriters(&out, &bytes.Buffer{})
	w.SetHint("Manage your agents \u2192 https://app.onecli.sh")

	data := map[string]string{"id": "abc"}
	if err := w.WriteFiltered(data, ""); err != nil {
		t.Fatal(err)
	}

	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if got["hint"] != "Manage your agents \u2192 https://app.onecli.sh" {
		t.Errorf("hint = %q", got["hint"])
	}
}

func TestErrorGoesToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	w := NewWithWriters(&stdout, &stderr)

	_ = w.Error("ERROR", "fail")

	if stdout.Len() != 0 {
		t.Error("error output should not go to stdout")
	}
	if stderr.Len() == 0 {
		t.Error("error output should go to stderr")
	}
}
