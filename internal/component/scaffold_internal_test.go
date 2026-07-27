package component

import (
	"strings"
	"testing"
)

// TestScaffoldCommandsMaskEverySecretEnv keeps the four command templates in step
// with scaffoldSecretEnv. Each language spells the mask differently, so a name
// added to the list but forgotten in one template would ship a scaffold that
// echoes that credential to stdout — the leak class #81 is about.
func TestScaffoldCommandsMaskEverySecretEnv(t *testing.T) {
	templates := map[string]string{
		"shell":  shellCommand,
		"python": pythonCommand,
		"node":   nodeCommand,
		"go":     goCommand,
	}
	for lang, tmpl := range templates {
		for _, name := range scaffoldSecretEnv {
			if !strings.Contains(tmpl, name) {
				t.Errorf("%s command scaffold does not mask %s", lang, name)
			}
		}
	}
}
