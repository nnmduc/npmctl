package cli

import "github.com/nnmduc/npmctl/internal/undo"

// newJournal opens the undo journal at its default location.
func newJournal() (*undo.Journal, error) { return undo.New() }
