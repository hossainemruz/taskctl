package domain

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func ParseTaskPrefix(value string) (TaskPrefix, error) {
	if value == "" {
		return "", fmt.Errorf("%w: Task prefix is empty", ErrInvalidID)
	}
	for index, character := range value {
		if index == 0 && (character < 'A' || character > 'Z') {
			return "", fmt.Errorf("%w: Task prefix must start with A-Z", ErrInvalidID)
		}
		if (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return "", fmt.Errorf("%w: Task prefix must contain only A-Z and 0-9", ErrInvalidID)
		}
	}
	return TaskPrefix(value), nil
}

func ParseTaskID(value string) (TaskPrefix, uint64, error) {
	separator := strings.LastIndexByte(value, '-')
	if separator <= 0 || separator == len(value)-1 {
		return "", 0, fmt.Errorf("%w: Task ID %q must be PREFIX-NNN", ErrInvalidID, value)
	}
	prefix, err := ParseTaskPrefix(value[:separator])
	if err != nil {
		return "", 0, fmt.Errorf("%w: Task ID %q: %v", ErrInvalidID, value, err)
	}
	number, err := parseSequence(value[separator+1:])
	if err != nil {
		return "", 0, fmt.Errorf("%w: Task ID %q: %v", ErrInvalidID, value, err)
	}
	formatted, _ := FormatTaskID(prefix, number)
	if string(formatted) != value {
		return "", 0, fmt.Errorf("%w: Task ID %q is not canonical", ErrInvalidID, value)
	}
	return prefix, number, nil
}

func FormatTaskID(prefix TaskPrefix, number uint64) (TaskID, error) {
	validPrefix, err := ParseTaskPrefix(string(prefix))
	if err != nil {
		return "", err
	}
	if number == 0 {
		return "", fmt.Errorf("%w: Task sequence must be positive", ErrInvalidID)
	}
	return TaskID(fmt.Sprintf("%s-%03d", validPrefix, number)), nil
}

func ParsePRID(value string) (uint64, error) {
	return parseScopedID(value, "PR")
}

func FormatPRID(number uint64) (PRID, error) {
	value, err := formatScopedID("PR", number)
	return PRID(value), err
}

func ParseStepID(value string) (uint64, error) {
	return parseScopedID(value, "STEP")
}

func FormatStepID(number uint64) (StepID, error) {
	value, err := formatScopedID("STEP", number)
	return StepID(value), err
}

func NextTaskID(prefix TaskPrefix, existing []TaskID) (TaskID, error) {
	if _, err := ParseTaskPrefix(string(prefix)); err != nil {
		return "", err
	}
	seen := make(map[TaskID]struct{}, len(existing))
	var maximum uint64
	for _, id := range existing {
		if _, duplicate := seen[id]; duplicate {
			return "", fmt.Errorf("%w: duplicate Task ID %s", ErrInvalidID, id)
		}
		seen[id] = struct{}{}
		foundPrefix, number, err := ParseTaskID(string(id))
		if err != nil {
			return "", err
		}
		if foundPrefix != prefix {
			return "", fmt.Errorf("%w: Task ID %s does not use prefix %s", ErrInvalidID, id, prefix)
		}
		if number > maximum {
			maximum = number
		}
	}
	if maximum == math.MaxUint64 {
		return "", ErrIDOverflow
	}
	return FormatTaskID(prefix, maximum+1)
}

func (t Task) NextPRID() (PRID, error) {
	seen := make(map[PRID]struct{}, len(t.PRs))
	var maximum uint64
	for prIndex := range t.PRs {
		id := t.PRs[prIndex].ID
		if _, duplicate := seen[id]; duplicate {
			return "", fmt.Errorf("%w: duplicate PR ID %s", ErrInvalidID, id)
		}
		seen[id] = struct{}{}
		number, err := ParsePRID(string(id))
		if err != nil {
			return "", err
		}
		if number > maximum {
			maximum = number
		}
	}
	if maximum == math.MaxUint64 {
		return "", ErrIDOverflow
	}
	return FormatPRID(maximum + 1)
}

func (t Task) NextStepID() (StepID, error) {
	seen := make(map[StepID]struct{})
	var maximum uint64
	for prIndex := range t.PRs {
		for stepIndex := range t.PRs[prIndex].Steps {
			id := t.PRs[prIndex].Steps[stepIndex].ID
			if _, duplicate := seen[id]; duplicate {
				return "", fmt.Errorf("%w: duplicate Step ID %s", ErrInvalidID, id)
			}
			seen[id] = struct{}{}
			number, err := ParseStepID(string(id))
			if err != nil {
				return "", err
			}
			if number > maximum {
				maximum = number
			}
		}
	}
	if maximum == math.MaxUint64 {
		return "", ErrIDOverflow
	}
	return FormatStepID(maximum + 1)
}

func parseScopedID(value, prefix string) (uint64, error) {
	wantedPrefix := prefix + "-"
	if !strings.HasPrefix(value, wantedPrefix) {
		return 0, fmt.Errorf("%w: %q must be %s-NNN", ErrInvalidID, value, prefix)
	}
	number, err := parseSequence(strings.TrimPrefix(value, wantedPrefix))
	if err != nil {
		return 0, fmt.Errorf("%w: %q: %v", ErrInvalidID, value, err)
	}
	formatted, _ := formatScopedID(prefix, number)
	if formatted != value {
		return 0, fmt.Errorf("%w: %q is not canonical", ErrInvalidID, value)
	}
	return number, nil
}

func formatScopedID(prefix string, number uint64) (string, error) {
	if number == 0 {
		return "", fmt.Errorf("%w: %s sequence must be positive", ErrInvalidID, prefix)
	}
	return fmt.Sprintf("%s-%03d", prefix, number), nil
}

func parseSequence(value string) (uint64, error) {
	if len(value) < 3 {
		return 0, fmt.Errorf("sequence must contain at least three digits")
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, fmt.Errorf("sequence must contain only digits")
		}
	}
	number, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("sequence overflows uint64")
	}
	if number == 0 {
		return 0, fmt.Errorf("sequence must be positive")
	}
	return number, nil
}
