package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/hossainemruz/taskctl/internal/domain"
)

// DecodeJSON decodes exactly one structured plan value. Domain validation is
// performed by the operation applying the plan because initial and evolved
// hierarchies have intentionally different Step-order constraints.
func DecodeJSON(reader io.Reader) (domain.Plan, error) {
	if reader == nil {
		return domain.Plan{}, fmt.Errorf("%w: input is unavailable", ErrInvalidInput)
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var input domain.Plan
	if err := decoder.Decode(&input); err != nil {
		if errors.Is(err, io.EOF) {
			return domain.Plan{}, fmt.Errorf("%w: input is empty", ErrInvalidInput)
		}
		return domain.Plan{}, fmt.Errorf("%w: decode JSON: %v", ErrInvalidInput, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return domain.Plan{}, fmt.Errorf("%w: input contains more than one JSON value", ErrInvalidInput)
		}
		return domain.Plan{}, fmt.Errorf("%w: decode trailing JSON: %v", ErrInvalidInput, err)
	}
	return input, nil
}
