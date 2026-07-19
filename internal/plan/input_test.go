package plan

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeJSONStrictlyDecodesOneValue(t *testing.T) {
	t.Parallel()
	input := `{"prs":[{"id":"PR-001","title":"Storage","steps":[{"id":"STEP-001","title":"Schema"}]}]}`
	decoded, err := DecodeJSON(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.PRs) != 1 || decoded.PRs[0].Steps[0].ID != "STEP-001" {
		t.Fatalf("DecodeJSON() = %#v", decoded)
	}

	for _, test := range []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "malformed", input: "{"},
		{name: "unknown top-level field", input: `{"prs":[],"status":"draft"}`},
		{name: "unknown nested field", input: `{"prs":[{"id":"PR-001","title":"Storage","branch":"x","steps":[]}]}`},
		{name: "trailing value", input: input + ` {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeJSON(strings.NewReader(test.input)); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("DecodeJSON() error = %v, want ErrInvalidInput", err)
			}
		})
	}
}
