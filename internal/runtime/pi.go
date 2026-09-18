package runtime

import (
	"context"
	"fmt"
	"strings"
)

// Pi returns the detector for the Pi coding agent.
func Pi() Detector {
	return NewCLIDetector("pi", "pi", []string{"--version"}, piConfigured)
}

// piConfigured asks Pi which models it can use. `pi --list-models` lists the
// models of the providers it has a key or a sign-in for, from its auth.json
// or the provider's environment variable, under a header row, and says "No
// models available" when there is none.
func piConfigured(ctx context.Context, bin string) (bool, error) {
	out, errOut, err := runCommandOutput(ctx, bin, "--list-models")
	if err != nil {
		return false, err
	}
	text := out
	if text == "" {
		text = errOut
	}
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	switch {
	case len(lines) > 0 && strings.HasPrefix(lines[0], "No models available"):
		return false, nil
	case len(lines) > 0 && strings.HasPrefix(lines[0], "provider"):
		return len(lines) > 1, nil
	}
	return false, fmt.Errorf("unexpected model list: %q", firstLine(text))
}
