package plan

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/hossainemruz/taskctl/internal/domain"
)

const (
	ProgressStartMarker = "<!-- taskctl:progress:start -->"
	ProgressEndMarker   = "<!-- taskctl:progress:end -->"
)

// RenderProgress renders the generated Task-wide PR/Step status list without
// markers. The caller owns placement inside plan.md's bounded block.
func RenderProgress(task domain.Task) string {
	if len(task.PRs) == 0 {
		return "_No plan has been applied._"
	}
	var rendered strings.Builder
	for prIndex, pr := range task.PRs {
		if prIndex > 0 {
			rendered.WriteByte('\n')
		}
		fmt.Fprintf(&rendered, "- %s: %s — %s", pr.ID, pr.Title, prStatusLabel(pr.Status()))
		for _, step := range pr.Steps {
			fmt.Fprintf(&rendered, "\n  - %s: %s — %s", step.ID, step.Title, stepStatusLabel(step.Status))
		}
	}
	return rendered.String()
}

// ReplaceProgress replaces only bytes strictly between one exact start marker
// and one exact end marker. Bytes on marker lines and outside the block are
// retained verbatim; generated line endings follow the start marker line.
func ReplaceProgress(markdown []byte, task domain.Task) ([]byte, error) {
	lines := splitLines(markdown)
	var starts, ends []lineSpan
	for _, line := range lines {
		switch string(markdown[line.contentStart:line.contentEnd]) {
		case ProgressStartMarker:
			starts = append(starts, line)
		case ProgressEndMarker:
			ends = append(ends, line)
		}
	}
	if len(starts) != 1 || len(ends) != 1 {
		return nil, fmt.Errorf("%w: want exactly one start and one end marker, found %d start and %d end markers", ErrInvalidMarkers, len(starts), len(ends))
	}
	start, end := starts[0], ends[0]
	if start.contentStart >= end.contentStart {
		return nil, fmt.Errorf("%w: end marker appears before start marker", ErrInvalidMarkers)
	}
	if start.lineEnd == start.contentEnd {
		return nil, fmt.Errorf("%w: start marker must be on a line before the end marker", ErrInvalidMarkers)
	}
	newline := markdown[start.contentEnd:start.lineEnd]
	if !bytes.Equal(newline, []byte("\n")) && !bytes.Equal(newline, []byte("\r\n")) {
		return nil, fmt.Errorf("%w: unsupported start marker line ending", ErrInvalidMarkers)
	}

	generated := strings.ReplaceAll(RenderProgress(task), "\n", string(newline))
	result := make([]byte, 0, len(markdown)+len(generated))
	result = append(result, markdown[:start.lineEnd]...)
	result = append(result, newline...)
	result = append(result, generated...)
	result = append(result, newline...)
	result = append(result, newline...)
	result = append(result, markdown[end.contentStart:]...)
	return result, nil
}

type lineSpan struct {
	contentStart int
	contentEnd   int
	lineEnd      int
}

func splitLines(contents []byte) []lineSpan {
	lines := make([]lineSpan, 0, bytes.Count(contents, []byte("\n"))+1)
	for start := 0; start < len(contents); {
		relativeEnd := bytes.IndexByte(contents[start:], '\n')
		if relativeEnd < 0 {
			lines = append(lines, lineSpan{contentStart: start, contentEnd: len(contents), lineEnd: len(contents)})
			return lines
		}
		newline := start + relativeEnd
		contentEnd := newline
		if contentEnd > start && contents[contentEnd-1] == '\r' {
			contentEnd--
		}
		lines = append(lines, lineSpan{contentStart: start, contentEnd: contentEnd, lineEnd: newline + 1})
		start = newline + 1
	}
	if len(contents) == 0 || contents[len(contents)-1] == '\n' {
		lines = append(lines, lineSpan{contentStart: len(contents), contentEnd: len(contents), lineEnd: len(contents)})
	}
	return lines
}

func prStatusLabel(status domain.PRStatus) string {
	switch status {
	case domain.PRInProgress:
		return "In Progress"
	case domain.PRCompleted:
		return "Completed"
	case domain.PRSkipped:
		return "Skipped"
	default:
		return "Pending"
	}
}

func stepStatusLabel(status domain.StepStatus) string {
	switch status {
	case domain.StepInProgress:
		return "In Progress"
	case domain.StepReadyForReview:
		return "Ready for Review"
	case domain.StepCompleted:
		return "Completed"
	case domain.StepSkipped:
		return "Skipped"
	default:
		return "Pending"
	}
}
