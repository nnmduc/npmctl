// `npmctl skill install` — writes the Agent Skill folder and optionally an AGENTS.md
// pointer.
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/nnmduc/npmctl/internal/skill"
	"github.com/spf13/cobra"
)

func newSkillCommand(f *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install the Agent Skill for AI coding agents",
		Long: "Writes an Agent Skill so Claude Code and similar agents can drive npmctl safely.\n\n" +
			"The skill pre-approves read commands and --dry-run only. No write command is\n" +
			"pre-approved, so every mutation re-enters the agent's normal permission flow.",
	}
	cmd.AddCommand(newSkillInstallCommand(f))
	return cmd
}

func newSkillInstallCommand(f *globalFlags) *cobra.Command {
	var dir, agentsMD string
	var withDocs bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write the skill folder (idempotent; never overwrites local edits)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			base, err := resolveSkillDir(dir)
			if err != nil {
				return err
			}
			destDir := filepath.Join(base, skill.Name)

			payload, err := skill.Files()
			if err != nil {
				return err
			}
			// The command reference is generated so it cannot drift from the binary.
			if withDocs {
				root, _ := NewRootCommand()
				payload = append(payload, skill.File{
					RelPath: "references/command-reference.md",
					Content: []byte(renderCommandReference(root)),
				})
			}

			manifest, err := skill.LoadManifest(destDir)
			if err != nil {
				return err
			}
			results, err := installFiles(destDir, payload, manifest, f.dryRun)
			if err != nil {
				return err
			}

			out := map[string]any{
				"skill":   skill.Name,
				"path":    destDir,
				"files":   results,
				"dry_run": f.dryRun,
			}
			if agentsMD != "" {
				note, err := updateAgentsFile(rt, agentsMD, f.dryRun, f.force)
				if err != nil {
					return err
				}
				out["agents_md"] = note
			}
			if preserved := countPreserved(results); preserved > 0 {
				fmt.Fprintf(rt.stderr,
					"%d file(s) had local edits and were left untouched; delete them to accept the shipped version\n",
					preserved)
			}
			return output.Render(rt.stdout, rt.format, out)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "skills directory (default: ~/.claude/skills)")
	cmd.Flags().StringVar(&agentsMD, "agents-md", "", "also append a pointer to this AGENTS.md")
	cmd.Flags().BoolVar(&withDocs, "with-command-reference", true, "generate references/command-reference.md")
	return cmd
}

func resolveSkillDir(dir string) (string, error) {
	if dir != "" {
		return expandUser(dir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "skills"), nil
}

// installFiles writes the payload, preserving locally modified files.

// installFiles writes the payload, preserving locally modified files.
func installFiles(destDir string, payload []skill.File, manifest *skill.Manifest, dryRun bool) ([]skill.Result, error) {
	results := make([]skill.Result, 0, len(payload))
	for _, file := range payload {
		action, err := manifest.Plan(destDir, file)
		if err != nil {
			return nil, err
		}
		results = append(results, skill.Result{RelPath: file.RelPath, Action: action})

		if dryRun {
			continue
		}
		// A locally modified file is reported, never clobbered — and its checksum is
		// left alone so the next run reaches the same conclusion.
		if action == skill.ActionPreserved {
			continue
		}
		if action == skill.ActionUnchanged {
			manifest.Checksums[file.RelPath] = skill.Sum(file.Content)
			continue
		}
		path := filepath.Join(destDir, filepath.FromSlash(file.RelPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, file.Content, 0o644); err != nil {
			return nil, exitcode.Wrap(exitcode.Generic, err, "write %s", path)
		}
		manifest.Checksums[file.RelPath] = skill.Sum(file.Content)
	}
	if dryRun {
		return results, nil
	}
	manifest.Version = version
	if err := manifest.Save(destDir); err != nil {
		return nil, err
	}
	return results, nil
}

func countPreserved(results []skill.Result) int {
	n := 0
	for _, r := range results {
		if r.Action == skill.ActionPreserved {
			n++
		}
	}
	return n
}

// agentsBlock is the pointer appended to AGENTS.md for agents that read that file
// instead of a skills directory.
