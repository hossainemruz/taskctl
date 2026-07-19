package vault

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

func decodeYAML(contents []byte, destination any, label string) error {
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	if err := decoder.Decode(destination); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: %s is empty", ErrInvalid, label)
		}
		return fmt.Errorf("%w: decode %s: %v", ErrInvalid, label, err)
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: %s contains multiple YAML documents", ErrInvalid, label)
		}
		return fmt.Errorf("%w: decode %s: %v", ErrInvalid, label, err)
	}
	return nil
}
