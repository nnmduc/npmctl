// The AGENTS.md pointer, for agents that read that file rather than a skills
// directory. Deduplication keys on a stable marker comment, so rewording the block never
// appends a second copy.
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// agentsMarker identifies npmctl's block in AGENTS.md.
//
// Deduplication keys on this marker rather than on a substring of the prose, so
// rewording the block does not cause a second copy to be appended.
const agentsMarker = "<!-- npmctl:nginx-proxy-manager -->"

// agentsBlock is the pointer appended to AGENTS.md for agents that read that file
// instead of a skills directory.
func agentsBlock() string {
	return agentsMarker + "\n" +
		"## Nginx Proxy Manager (npmctl)\n\n" +
		"Manage Nginx Proxy Manager with the `npmctl` CLI.\n\n" +
		"- Read first: `npmctl host list`, `npmctl host get <id|domain>`.\n" +
		"- Preview every mutation with `--dry-run`; it issues no mutating request.\n" +
		"- Writes require BOTH `NPMCTL_ALLOW_WRITE=1` and `--yes`.\n" +
		"- In interactive mode, do not set `NPMCTL_ALLOW_WRITE` yourself (human decides).\n" +
		"- Automation / CI/CD: When invoked with `--ignore-safe-mode`, execute writes automatically without interaction.\n" +
		"- Exit 3 means refused: surface it, do not work around it. Exit 9 means a human\n" +
		"  must re-authenticate. Exit 8 means the write applied but nginx is unhealthy.\n" +
		"- Never retry a failed certificate operation, and never use `advanced_config`.\n" +
		"- `cert rm` revokes a Let's Encrypt certificate irreversibly.\n"
}

// updateAgentsFile appends the pointer block, skipping when the marker already exists.

// updateAgentsFile appends the pointer block, skipping when the marker already exists.
func updateAgentsFile(rt *runtime, path string, dryRun, force bool) (string, error) {
	full := expandUser(path)
	existing, err := os.ReadFile(full)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if strings.Contains(string(existing), agentsMarker) {
		return "already present (marker found); left unchanged", nil
	}

	block := agentsBlock()
	// AGENTS.md lives in the caller's working tree and is very likely tracked by git,
	// so show what would change and require confirmation.
	fmt.Fprintf(rt.stderr, "would append to %s:\n\n%s\n", full, block)
	if dryRun {
		return "not modified (--dry-run)", nil
	}
	if !force {
		return "", exitcode.New(exitcode.Refused,
			"refusing to modify %s without --force: review the block above first", full)
	}

	content := string(existing)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if content != "" {
		content += "\n"
	}
	content += block
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return "", err
	}
	return "appended", nil
}
