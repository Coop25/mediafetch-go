package mediafetch

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %v: %w: %s", name, args, err, strings.TrimSpace(string(output)))
	}
	return output, nil
}
