// The item-level diff behind a full-replacement access-list update, including the
// refusal that prevents silently blanking an existing user's password.
package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/npmapi"
)

// aclItemDiff classifies what a replacement items array does to existing users.
type aclItemDiff struct {
	Added   []string
	Removed []string
	Kept    []string
	Reset   []string // existing username supplied with an empty password
}

// diffACLItems compares the stored items against the replacement set.

// diffACLItems compares the stored items against the replacement set.
func diffACLItems(current []npmapi.AccessListItem, next []npmapi.AccessListItem) *aclItemDiff {
	d := &aclItemDiff{}
	existing := map[string]bool{}
	for _, it := range current {
		existing[it.Username] = true
	}
	incoming := map[string]bool{}
	for _, it := range next {
		incoming[it.Username] = true
		switch {
		case !existing[it.Username]:
			d.Added = append(d.Added, it.Username)
		case it.Password == "":
			d.Reset = append(d.Reset, it.Username)
		default:
			d.Kept = append(d.Kept, it.Username)
		}
	}
	for _, it := range current {
		if !incoming[it.Username] {
			d.Removed = append(d.Removed, it.Username)
		}
	}
	for _, s := range [][]string{d.Added, d.Removed, d.Kept, d.Reset} {
		sort.Strings(s)
	}
	return d
}

func (d *aclItemDiff) render() string {
	var b strings.Builder
	for _, u := range d.Added {
		fmt.Fprintf(&b, "  ADDED           %s\n", u)
	}
	for _, u := range d.Kept {
		fmt.Fprintf(&b, "  PASSWORD SET    %s\n", u)
	}
	for _, u := range d.Reset {
		fmt.Fprintf(&b, "  PASSWORD RESET  %s  <-- would be blanked\n", u)
	}
	for _, u := range d.Removed {
		fmt.Fprintf(&b, "  REMOVED         %s\n", u)
	}
	if b.Len() == 0 {
		return "  (no item changes)\n"
	}
	return b.String()
}

// refuseIfDestructive blocks the specific silent failure: an existing user resubmitted
// with an empty password. NPM accepts it and answers 200, leaving that user with
// blank credentials.

// refuseIfDestructive blocks the specific silent failure: an existing user resubmitted
// with an empty password. NPM accepts it and answers 200, leaving that user with
// blank credentials.
func (d *aclItemDiff) refuseIfDestructive() error {
	if len(d.Reset) == 0 {
		return nil
	}
	return exitcode.New(exitcode.Refused,
		"refusing to update: %s would have their password blanked. "+
			"NPM does not return existing passwords, so supply each user's real password with "+
			"--item user:password, or omit the user to remove them deliberately.",
		strings.Join(d.Reset, ", "))
}
