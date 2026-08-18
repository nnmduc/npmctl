package cli

import (
	"encoding/json"
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/nnmduc/npmctl/internal/undo"
	"github.com/spf13/cobra"
)

func newUndoCommand(f *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "undo",
		Short: "Inspect and replay pre-images captured before each write",
		Long: "npmctl records the prior state of every object it mutates, before mutating it.\n\n" +
			"`undo list` and `undo show` are reads. `undo apply` is a WRITE: it replays the\n" +
			"pre-image through the same gate as any other mutation.\n\n" +
			"Entries are stored unredacted at mode 0600 and swept after 30 days — a redacted\n" +
			"pre-image could not be restored. Treat the directory as sensitive.",
	}
	cmd.AddCommand(newUndoListCommand(f), newUndoShowCommand(f), newUndoApplyCommand(f))
	return cmd
}

func newUndoListCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List captured pre-images for the active profile, newest first",
		RunE: func(_ *cobra.Command, _ []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			entries, err := rt.journal.List(rt.profileName)
			if err != nil {
				return err
			}
			rows := make([]map[string]any, 0, len(entries))
			for _, e := range entries {
				rows = append(rows, map[string]any{
					"id": e.ID, "time": e.Time, "verb": e.Verb,
					"resource": e.Resource, "kind": e.Kind, "note": e.Note,
				})
			}
			return output.RenderWith(rt.stdout, rt.format, []output.Column{
				{Header: "ENTRY", Key: "id"},
				{Header: "TIME", Key: "time"},
				{Header: "VERB", Key: "verb"},
				{Header: "RESOURCE", Key: "resource"},
			}, rows)
		},
	}
}

func newUndoShowCommand(f *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "show <entry>",
		Short: "Render a captured pre-image (redacted for display)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			e, err := loadEntry(rt, args[0])
			if err != nil {
				return err
			}
			var pre any
			if err := json.Unmarshal(e.PreImage, &pre); err != nil {
				return err
			}
			// Rendered through the output funnel, so display is redacted even
			// though the file on disk is not.
			return output.Render(rt.stdout, rt.format, map[string]any{
				"id": e.ID, "time": e.Time, "profile": e.Profile, "url": e.URL,
				"verb": e.Verb, "resource": e.Resource, "kind": e.Kind,
				"note": e.Note, "pre_image": pre,
			})
		},
	}
}

func loadEntry(rt *runtime, ref string) (*undo.Entry, error) {
	e, err := rt.journal.Load(rt.profileName, ref)
	if err != nil {
		return nil, exitcode.Wrap(exitcode.NotFound, err, "no journal entry %q for profile %q", ref, rt.profileName)
	}
	// R10 scoping applies to the journal too: a pre-image captured against one
	// instance must never be replayed against another.
	if e.Profile != rt.profileName {
		return nil, exitcode.New(exitcode.Refused,
			"entry %s belongs to profile %q, not %q", e.ID, e.Profile, rt.profileName)
	}
	if want := strings.TrimRight(rt.profile.URL, "/"); e.URL != "" && want != "" && e.URL != want {
		return nil, exitcode.New(exitcode.Refused,
			"entry %s was captured against %s but profile %q now points at %s — refusing to replay it",
			e.ID, e.URL, rt.profileName, want)
	}
	return e, nil
}
