package plan

import (
	"fmt"
	"strings"

	"github.com/hossainemruz/taskctl/internal/domain"
)

type headingKind uint8

const (
	headingPR headingKind = iota + 1
	headingStep
)

type heading struct {
	kind   headingKind
	id     string
	title  string
	parent string
	line   int
}

// ValidateHeadings requires plan.md's exact structured headings to correspond
// one-for-one with the supplied hierarchy, including source order and parents.
// It deliberately does not infer a hierarchy from any other Markdown content.
func ValidateHeadings(markdown []byte, hierarchy domain.Plan) error {
	headings, err := scanHeadings(markdown)
	if err != nil {
		return err
	}
	expected, prTitles, stepDetails := expectedHeadings(hierarchy)

	seenPRs := make(map[string]int)
	seenSteps := make(map[string]int)
	for _, found := range headings {
		switch found.kind {
		case headingPR:
			title, registered := prTitles[found.id]
			if !registered {
				return headingError(found.line, found.id, "heading is not registered by the structured plan")
			}
			if first, duplicate := seenPRs[found.id]; duplicate {
				return headingError(found.line, found.id, "duplicate heading (first appears on line %d)", first)
			}
			seenPRs[found.id] = found.line
			if found.title != title {
				return headingError(found.line, found.id, "title %q does not match structured title %q", found.title, title)
			}
		case headingStep:
			detail, registered := stepDetails[found.id]
			if !registered {
				return headingError(found.line, found.id, "heading is not registered by the structured plan")
			}
			if first, duplicate := seenSteps[found.id]; duplicate {
				return headingError(found.line, found.id, "duplicate heading (first appears on line %d)", first)
			}
			seenSteps[found.id] = found.line
			if found.parent != detail.parent {
				return headingError(found.line, found.id, "is under %s, want %s", displayParent(found.parent), detail.parent)
			}
			if found.title != detail.title {
				return headingError(found.line, found.id, "title %q does not match structured title %q", found.title, detail.title)
			}
		}
	}
	for _, wanted := range expected {
		seen := seenPRs
		if wanted.kind == headingStep {
			seen = seenSteps
		}
		if _, found := seen[wanted.id]; !found {
			return headingError(0, wanted.id, "heading is missing from plan.md")
		}
	}
	if len(headings) != len(expected) {
		return fmt.Errorf("%w: structured heading count %d does not match expected count %d", ErrInvalidHeadings, len(headings), len(expected))
	}
	for index := range expected {
		if headings[index].kind == expected[index].kind && headings[index].id == expected[index].id {
			continue
		}
		return headingError(headings[index].line, headings[index].id,
			"is out of order; want %s at structured heading position %d", expected[index].id, index+1)
	}
	return nil
}

type expectedStep struct {
	title  string
	parent string
}

func expectedHeadings(hierarchy domain.Plan) ([]heading, map[string]string, map[string]expectedStep) {
	ordered := make([]heading, 0)
	prs := make(map[string]string, len(hierarchy.PRs))
	steps := make(map[string]expectedStep)
	for _, pr := range hierarchy.PRs {
		prID := string(pr.ID)
		prs[prID] = pr.Title
		ordered = append(ordered, heading{kind: headingPR, id: prID})
		for _, step := range pr.Steps {
			stepID := string(step.ID)
			steps[stepID] = expectedStep{title: step.Title, parent: prID}
			ordered = append(ordered, heading{kind: headingStep, id: stepID, parent: prID})
		}
	}
	return ordered, prs, steps
}

func scanHeadings(markdown []byte) ([]heading, error) {
	lines := strings.Split(string(markdown), "\n")
	result := make([]heading, 0)
	currentPR := ""
	for index, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		lineNumber := index + 1
		if strings.HasPrefix(line, "### PR-") {
			id, title, err := parseHeadingLine(line, "### ", "PR", lineNumber)
			if err != nil {
				return nil, err
			}
			currentPR = id
			result = append(result, heading{kind: headingPR, id: id, title: title, line: lineNumber})
			continue
		}
		if strings.HasPrefix(line, "#### STEP-") {
			id, title, err := parseHeadingLine(line, "#### ", "STEP", lineNumber)
			if err != nil {
				return nil, err
			}
			if currentPR == "" {
				return nil, headingError(lineNumber, id, "appears before any PR heading")
			}
			result = append(result, heading{kind: headingStep, id: id, title: title, parent: currentPR, line: lineNumber})
			continue
		}
		if kind := malformedStructuredHeading(line); kind != "" {
			return nil, headingError(lineNumber, kind, "must use exact heading form %s", exactHeadingForm(kind))
		}
	}
	return result, nil
}

func parseHeadingLine(line, prefix, kind string, lineNumber int) (string, string, error) {
	remainder := strings.TrimPrefix(line, prefix)
	id, title, found := strings.Cut(remainder, ": ")
	if !found || title == "" {
		return "", "", headingError(lineNumber, kind, "must use exact heading form %s", exactHeadingForm(kind))
	}
	var err error
	switch kind {
	case "PR":
		_, err = domain.ParsePRID(id)
	case "STEP":
		_, err = domain.ParseStepID(id)
	}
	if err != nil {
		return "", "", headingError(lineNumber, id, "invalid identifier: %v", err)
	}
	return id, title, nil
}

func malformedStructuredHeading(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	hashes := 0
	for hashes < len(trimmed) && trimmed[hashes] == '#' {
		hashes++
	}
	if hashes == 0 || hashes == len(trimmed) || trimmed[hashes] != ' ' {
		return ""
	}
	text := trimmed[hashes+1:]
	if strings.HasPrefix(text, "PR-") {
		return "PR"
	}
	if strings.HasPrefix(text, "STEP-") {
		return "STEP"
	}
	return ""
}

func exactHeadingForm(kind string) string {
	if kind == "PR" {
		return "### PR-NNN: Title"
	}
	return "#### STEP-NNN: Title"
}

func displayParent(parent string) string {
	if parent == "" {
		return "no PR"
	}
	return parent
}

func headingError(line int, id, format string, args ...any) error {
	problem := fmt.Sprintf(format, args...)
	if line > 0 {
		return fmt.Errorf("%w: line %d: %s: %s", ErrInvalidHeadings, line, id, problem)
	}
	return fmt.Errorf("%w: %s: %s", ErrInvalidHeadings, id, problem)
}
