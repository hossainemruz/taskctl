package templates

import "embed"

//go:embed defaults/*.tmpl
var defaults embed.FS

var defaultNames = []string{
	"task.md.tmpl",
	"research.md.tmpl",
	"plan.md.tmpl",
	"review.md.tmpl",
}

// DefaultNames returns default template names in installation order.
func DefaultNames() []string {
	return append([]string(nil), defaultNames...)
}

// ReadDefault returns one embedded default template.
func ReadDefault(name string) ([]byte, error) {
	return defaults.ReadFile("defaults/" + name)
}
