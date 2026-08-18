package cli

import (
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/npmapi"
	"github.com/spf13/cobra"
)

// aclFlags mirrors the access-list write schema.
//
// There is deliberately NO --add-item or --remove-item in v1. Those would require
// read-modify-write, and GET never returns passwords — it returns password: "" for
// every user. Writing that back sets every existing user's password to empty, and
// the schema imposes no minLength, so the API accepts it and returns 200. Three
// real users would silently end up with blank credentials.
type aclFlags struct {
	name       string
	satisfyAny bool
	passAuth   bool
	items      []string // username:password
	clients    []string // directive:address
}

func (a *aclFlags) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.StringVar(&a.name, "name", "", "access list name")
	fl.BoolVar(&a.satisfyAny, "satisfy-any", false, "pass if ANY rule matches (default: ALL must match)")
	fl.BoolVar(&a.passAuth, "pass-auth", false, "forward the Authorization header upstream")
	fl.StringArrayVar(&a.items, "item", nil, "basic-auth user as username:password (repeatable)")
	fl.StringArrayVar(&a.clients, "client", nil, "IP rule as directive:address, e.g. allow:192.168.0.0/24 (repeatable)")
}

// parseItems converts --item values. Passwords are required: an empty one is the
// exact failure mode this command exists to prevent.

// parseItems converts --item values. Passwords are required: an empty one is the
// exact failure mode this command exists to prevent.
func parseItems(raw []string) ([]npmapi.AccessListItem, error) {
	out := make([]npmapi.AccessListItem, 0, len(raw))
	for _, s := range raw {
		user, pass, found := strings.Cut(s, ":")
		user = strings.TrimSpace(user)
		if !found || user == "" {
			return nil, exitcode.New(exitcode.Usage,
				"--item %q is not username:password", s)
		}
		if pass == "" {
			return nil, exitcode.New(exitcode.Usage,
				"--item %q has an empty password. NPM would accept it and blank that user's "+
					"credentials; supply the real password, or drop the user from the list.", s)
		}
		out = append(out, npmapi.AccessListItem{Username: user, Password: pass})
	}
	return out, nil
}

func parseClients(raw []string) ([]npmapi.AccessListClient, error) {
	out := make([]npmapi.AccessListClient, 0, len(raw))
	for _, s := range raw {
		directive, address, found := strings.Cut(s, ":")
		directive = strings.ToLower(strings.TrimSpace(directive))
		address = strings.TrimSpace(address)
		if !found || address == "" {
			return nil, exitcode.New(exitcode.Usage, "--client %q is not directive:address", s)
		}
		if directive != "allow" && directive != "deny" {
			return nil, exitcode.New(exitcode.Usage,
				"--client %q: directive must be allow or deny, got %q", s, directive)
		}
		out = append(out, npmapi.AccessListClient{Directive: directive, Address: address})
	}
	return out, nil
}

func (a *aclFlags) payload(cmd *cobra.Command) (*npmapi.Payload, error) {
	p := npmapi.NewAccessListPayload()
	fl := cmd.Flags()
	p.SetIf(fl.Changed("name"), "name", a.name)
	p.SetIf(fl.Changed("satisfy-any"), "satisfy_any", a.satisfyAny)
	p.SetIf(fl.Changed("pass-auth"), "pass_auth", a.passAuth)

	if fl.Changed("item") {
		items, err := parseItems(a.items)
		if err != nil {
			return nil, err
		}
		p.Set("items", items)
	}
	if fl.Changed("client") {
		clients, err := parseClients(a.clients)
		if err != nil {
			return nil, err
		}
		p.Set("clients", clients)
	}
	return p, nil
}

func newACLCommand(f *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "acl",
		Aliases: []string{"access-list"},
		Short:   "Manage access lists",
		Long: "Access lists combine HTTP basic auth with IP allow/deny rules.\n\n" +
			"IMPORTANT: `acl update` REPLACES the items and clients arrays wholesale — it does\n" +
			"not merge. NPM never returns existing passwords (it returns an empty string), so\n" +
			"npmctl cannot reconstruct them for you. You must pass every user you want to keep,\n" +
			"with their password. Any user you omit is removed.\n\n" +
			"Use --dry-run to see exactly which users would be added, removed, or have their\n" +
			"password reset before anything is written.",
	}
	cmd.AddCommand(
		newACLListCommand(f), newACLGetCommand(f),
		newACLCreateCommand(f), newACLUpdateCommand(f), newACLRemoveCommand(f),
	)
	return cmd
}

// resolveACL accepts a numeric ID or a name.
