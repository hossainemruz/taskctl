package plan

import (
	"errors"
	"strings"
	"testing"

	"github.com/hossainemruz/taskctl/internal/domain"
)

func headingTestPlan() domain.Plan {
	return domain.Plan{PRs: []domain.PlannedPR{
		{ID: "PR-001", Title: "Storage", Steps: []domain.PlannedStep{
			{ID: "STEP-001", Title: "Schema"},
			{ID: "STEP-002", Title: "Persistence"},
		}},
		{ID: "PR-002", Title: "CLI", Steps: []domain.PlannedStep{{ID: "STEP-003", Title: "Commands"}}},
	}}
}

func TestValidateHeadingsAcceptsExactOrderedHierarchy(t *testing.T) {
	t.Parallel()
	markdown := strings.Join([]string{
		"# Plan",
		"",
		"### PR-001: Storage",
		"Prose.",
		"#### STEP-001: Schema",
		"#### STEP-002: Persistence",
		"### PR-002: CLI",
		"#### STEP-003: Commands",
		"",
	}, "\r\n")
	if err := ValidateHeadings([]byte(markdown), headingTestPlan()); err != nil {
		t.Fatalf("ValidateHeadings() error = %v", err)
	}
}

func TestValidateHeadingsRejectsMismatchWithLine(t *testing.T) {
	t.Parallel()
	valid := "### PR-001: Storage\n#### STEP-001: Schema\n#### STEP-002: Persistence\n### PR-002: CLI\n#### STEP-003: Commands\n"
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "missing", text: strings.Replace(valid, "#### STEP-002: Persistence\n", "", 1), want: "STEP-002"},
		{name: "duplicate", text: strings.Replace(valid, "#### STEP-002: Persistence", "#### STEP-001: Schema\n#### STEP-002: Persistence", 1), want: "line 3"},
		{name: "wrong parent", text: "### PR-001: Storage\n#### STEP-001: Schema\n### PR-002: CLI\n#### STEP-002: Persistence\n#### STEP-003: Commands\n", want: "want PR-001"},
		{name: "wrong order", text: "### PR-001: Storage\n#### STEP-002: Persistence\n#### STEP-001: Schema\n### PR-002: CLI\n#### STEP-003: Commands\n", want: "out of order"},
		{name: "title mismatch", text: strings.Replace(valid, "#### STEP-001: Schema", "#### STEP-001: Different", 1), want: "line 2"},
		{name: "unregistered", text: valid + "### PR-003: Extra\n", want: "not registered"},
		{name: "malformed exact form", text: strings.Replace(valid, "#### STEP-001: Schema", "### STEP-001: Schema", 1), want: "exact heading form"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateHeadings([]byte(test.text), headingTestPlan())
			if !errors.Is(err, ErrInvalidHeadings) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateHeadings() error = %v, want ErrInvalidHeadings containing %q", err, test.want)
			}
		})
	}
}
