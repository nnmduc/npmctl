package cli

import (
	"context"

	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/spf13/cobra"
)

// crudSpec describes one NPM collection well enough to generate its whole command
// group. It exists because redirection-hosts, dead-hosts and streams share an
// identical 7-operation shape; writing the gate plumbing three more times would
// mean three more chances to forget a step.
//
// What it does NOT share is the request bodies — those come from each resource's
// own payload builder, because the schemas genuinely differ.
type crudSpec[T any] struct {
	use     string // command name, e.g. "redirect"
	aliases []string
	short   string
	long    string
	kind    string // resource kind used in messages and journal entries
	path    string // API path, for Op.Path
	columns []output.Column

	// refHelp describes what a positional reference may be, e.g. "<id|domain>".
	refHelp string

	registerFlags func(*cobra.Command)
	createPayload func(*cobra.Command) (*npmapi.Payload, error)
	updatePayload func(*cobra.Command) (*npmapi.Payload, error)

	resolve func(context.Context, *npmapi.Client, string) (*T, error)
	list    func(context.Context, *npmapi.Client) ([]T, error)
	get     func(context.Context, *npmapi.Client, int) (*T, error)
	create  func(context.Context, *npmapi.Client, map[string]any) (*T, error)
	update  func(context.Context, *npmapi.Client, int, map[string]any) (*T, error)
	remove  func(context.Context, *npmapi.Client, int) error

	// setEnabled is nil for resources with no enable/disable endpoint.
	setEnabled func(context.Context, *npmapi.Client, int, bool) error

	idOf       func(*T) int
	nameOf     func(*T) string
	modifiedOf func(*T) string
	metaOf     func(*T) npmapi.Meta

	// advancedConfigOf returns the current raw nginx block, or "" when the resource
	// has no such field.
	advancedConfigOf func(*T) string

	// dependents lists objects that would be affected by deleting this one.
	dependents func(context.Context, *npmapi.Client, *T) ([]string, error)

	// deleteTier and deleteNote let a resource escalate its own delete, which is how
	// `cert rm` carries its irreversibility warning.
	deleteTier Tier
	deleteNote string
}

// newCRUDCommand builds the command group for one collection.
func newCRUDCommand[T any](f *globalFlags, spec crudSpec[T]) *cobra.Command {
	cmd := &cobra.Command{
		Use:     spec.use,
		Aliases: spec.aliases,
		Short:   spec.short,
		Long:    spec.long,
	}
	cmd.AddCommand(newCRUDListCommand(f, spec), newCRUDGetCommand(f, spec))
	if spec.create != nil {
		cmd.AddCommand(newCRUDCreateCommand(f, spec))
	}
	if spec.update != nil {
		cmd.AddCommand(newCRUDUpdateCommand(f, spec))
	}
	if spec.remove != nil {
		cmd.AddCommand(newCRUDRemoveCommand(f, spec))
	}
	if spec.setEnabled != nil {
		cmd.AddCommand(newCRUDToggleCommand(f, spec, true), newCRUDToggleCommand(f, spec, false))
	}
	return cmd
}

func newCRUDListCommand[T any](f *globalFlags, spec crudSpec[T]) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List " + spec.kind + "s",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			items, err := spec.list(cmd.Context(), c)
			if err != nil {
				return err
			}
			return output.RenderWith(rt.stdout, rt.format, spec.columns, items)
		},
	}
}

func newCRUDGetCommand[T any](f *globalFlags, spec crudSpec[T]) *cobra.Command {
	return &cobra.Command{
		Use:   "get " + spec.refHelp,
		Short: "Show one " + spec.kind,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, c, err := authedRuntime(f)
			if err != nil {
				return err
			}
			item, err := spec.resolve(cmd.Context(), c, args[0])
			if err != nil {
				return err
			}
			return output.Render(rt.stdout, rt.format, item)
		},
	}
}

// authedRuntime is the common preamble: load configuration, then obtain a client
// carrying a valid token.
func authedRuntime(f *globalFlags) (*runtime, *npmapi.Client, error) {
	rt, err := newRuntime(f)
	if err != nil {
		return nil, nil, err
	}
	c, err := rt.authenticated()
	if err != nil {
		return nil, nil, err
	}
	return rt, c, nil
}
