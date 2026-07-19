package domain

import (
	"errors"
	"testing"
)

func validPlan() Plan {
	return Plan{PRs: []PlannedPR{
		{
			ID: "PR-001", Title: "Storage",
			Steps: []PlannedStep{
				{ID: "STEP-001", Title: "Schema"},
				{ID: "STEP-002", Title: "Persistence"},
			},
		},
		{
			ID: "PR-002", Title: "CLI",
			Steps: []PlannedStep{{ID: "STEP-003", Title: "Commands"}},
		},
	}}
}

func TestPlanValidateAndMaterialize(t *testing.T) {
	t.Parallel()
	plan := validPlan()
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	prs, err := plan.PRModels()
	if err != nil {
		t.Fatalf("PRModels() error = %v", err)
	}
	if len(prs) != 2 || len(prs[0].Steps) != 2 || prs[1].Steps[0].Status != StepPending {
		t.Fatalf("PRModels() = %#v", prs)
	}
	prs[0].Title = "Changed"
	if plan.PRs[0].Title == "Changed" {
		t.Fatal("PRModels() shares storage with Plan")
	}
}

func TestPlanValidateRejectsInvalidHierarchy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*Plan)
	}{
		{name: "no PRs", mutate: func(plan *Plan) { plan.PRs = nil }},
		{name: "empty PR", mutate: func(plan *Plan) { plan.PRs[0].Steps = nil }},
		{name: "empty PR title", mutate: func(plan *Plan) { plan.PRs[0].Title = " \t" }},
		{name: "malformed PR ID", mutate: func(plan *Plan) { plan.PRs[0].ID = "PR-01" }},
		{name: "duplicate PR ID", mutate: func(plan *Plan) { plan.PRs[1].ID = "PR-001" }},
		{name: "PR gap", mutate: func(plan *Plan) { plan.PRs[1].ID = "PR-003" }},
		{name: "empty Step title", mutate: func(plan *Plan) { plan.PRs[0].Steps[0].Title = "" }},
		{name: "malformed Step ID", mutate: func(plan *Plan) { plan.PRs[0].Steps[0].ID = "STEP-01" }},
		{name: "duplicate Step ID across PRs", mutate: func(plan *Plan) { plan.PRs[1].Steps[0].ID = "STEP-001" }},
		{name: "Step gap", mutate: func(plan *Plan) { plan.PRs[0].Steps[1].ID = "STEP-003" }},
		{name: "Step order", mutate: func(plan *Plan) {
			plan.PRs[0].Steps[0].ID = "STEP-002"
			plan.PRs[0].Steps[1].ID = "STEP-001"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			plan := validPlan()
			test.mutate(&plan)
			if err := plan.Validate(); !errors.Is(err, ErrInvalidState) {
				t.Fatalf("Validate() error = %v, want ErrInvalidState", err)
			}
		})
	}
}
