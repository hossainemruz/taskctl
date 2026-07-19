package domain

import "fmt"

// Plan contains only the operational hierarchy supplied by planning. It has no
// lifecycle or prose fields.
type Plan struct {
	PRs []PlannedPR `json:"prs"`
}

type PlannedPR struct {
	ID    PRID          `json:"id"`
	Title string        `json:"title"`
	Steps []PlannedStep `json:"steps"`
}

type PlannedStep struct {
	ID    StepID `json:"id"`
	Title string `json:"title"`
}

func (p Plan) Validate() error {
	return p.validate(true)
}

// validateEvolved validates a hierarchy that may have gained a later Step in
// an earlier PR. Initial plans require Task-wide Step IDs to follow hierarchy
// order; append-only evolution can make that impossible while still preserving
// unique, contiguous IDs and order within each PR.
func (p Plan) validateEvolved() error {
	return p.validate(false)
}

func (p Plan) validate(requireStepTraversalOrder bool) error {
	if len(p.PRs) == 0 {
		return invalid("prs", "plan must contain at least one PR")
	}
	prIDs := make(map[PRID]struct{}, len(p.PRs))
	stepIDs := make(map[StepID]struct{})
	stepNumbers := make(map[uint64]struct{})
	nextStep := uint64(1)
	for prIndex := range p.PRs {
		path := fmt.Sprintf("prs[%d]", prIndex)
		pr := p.PRs[prIndex]
		if _, exists := prIDs[pr.ID]; exists {
			return invalid(path+".id", "duplicate PR ID %s", pr.ID)
		}
		prIDs[pr.ID] = struct{}{}
		prNumber, err := ParsePRID(string(pr.ID))
		if err != nil {
			return invalid(path+".id", "%v", err)
		}
		if prNumber != uint64(prIndex+1) {
			expected, _ := FormatPRID(uint64(prIndex + 1))
			return invalid(path+".id", "got %s, want %s to preserve sequential order", pr.ID, expected)
		}
		if err := validateTitle(path+".title", pr.Title); err != nil {
			return err
		}
		if len(pr.Steps) == 0 {
			return invalid(path+".steps", "PR must contain at least one Step")
		}
		var previousStep uint64
		for stepIndex := range pr.Steps {
			stepPath := fmt.Sprintf("%s.steps[%d]", path, stepIndex)
			step := pr.Steps[stepIndex]
			if _, exists := stepIDs[step.ID]; exists {
				return invalid(stepPath+".id", "duplicate Step ID %s", step.ID)
			}
			stepIDs[step.ID] = struct{}{}
			stepNumber, err := ParseStepID(string(step.ID))
			if err != nil {
				return invalid(stepPath+".id", "%v", err)
			}
			if requireStepTraversalOrder && stepNumber != nextStep {
				expected, _ := FormatStepID(nextStep)
				return invalid(stepPath+".id", "got %s, want %s to preserve Task-wide sequential order", step.ID, expected)
			}
			if !requireStepTraversalOrder && stepNumber <= previousStep {
				return invalid(stepPath+".id", "Step IDs must increase within a PR")
			}
			if _, exists := stepNumbers[stepNumber]; exists {
				return invalid(stepPath+".id", "duplicate Step sequence %s", step.ID)
			}
			stepNumbers[stepNumber] = struct{}{}
			previousStep = stepNumber
			if err := validateTitle(stepPath+".title", step.Title); err != nil {
				return err
			}
			nextStep++
		}
	}
	for expected := uint64(1); expected <= uint64(len(stepIDs)); expected++ {
		if _, exists := stepNumbers[expected]; !exists {
			id, _ := FormatStepID(expected)
			return invalid("prs.steps", "missing sequential Step ID %s", id)
		}
	}
	return nil
}

// PRModels validates and materializes a fresh pending hierarchy.
func (p Plan) PRModels() ([]PR, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	prs := make([]PR, len(p.PRs))
	for prIndex := range p.PRs {
		plannedPR := p.PRs[prIndex]
		prs[prIndex] = PR{
			ID:    plannedPR.ID,
			Title: plannedPR.Title,
			Steps: make([]Step, len(plannedPR.Steps)),
		}
		for stepIndex := range plannedPR.Steps {
			plannedStep := plannedPR.Steps[stepIndex]
			prs[prIndex].Steps[stepIndex] = Step{
				ID:     plannedStep.ID,
				Title:  plannedStep.Title,
				Status: StepPending,
			}
		}
	}
	return prs, nil
}
