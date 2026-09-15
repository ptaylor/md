package pager

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ShowExternal pipes the document through the user's own pager: $MD_PAGER
// first, then $PAGER, then less.
//
// md deliberately does not add flags to a pager the user chose, but its own
// fallback uses "less -R" because less otherwise shows the colour escapes as
// literal text.
func ShowExternal(content, title string) error {
	cmdline := os.Getenv("MD_PAGER")
	if cmdline == "" {
		cmdline = os.Getenv("PAGER")
	}
	if cmdline == "" {
		cmdline = "less -R"
	}
	fields := strings.Fields(cmdline)
	if len(fields) == 0 {
		return fmt.Errorf("empty pager command")
	}
	if _, err := exec.LookPath(fields[0]); err != nil {
		return fmt.Errorf("pager %q not found", fields[0])
	}
	cmd := exec.Command("/bin/sh", "-c", cmdline) //nolint:gosec // the user's own pager
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running %s: %w", fields[0], err)
	}
	return nil
}
