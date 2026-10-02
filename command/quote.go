package command

import "strings"

// POSIXQuote quotes one argument for a POSIX shell (sh/bash/zsh).
// It is not suitable for cmd or PowerShell.
func POSIXQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
