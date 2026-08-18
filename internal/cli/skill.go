// `npmctl skill install` — writes the Agent Skill folder and optionally an AGENTS.md
// pointer for Claude Code, Antigravity (AGY), Codex, OpenCode, Cursor, and other AI agents.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
	"github.com/nnmduc/npmctl/internal/output"
	"github.com/nnmduc/npmctl/internal/skill"
	"github.com/spf13/cobra"
)

// Agent defines a supported AI coding agent and how to locate its skill directories.
type Agent struct {
	ID          string
	DisplayName string
	Aliases     []string
	GlobalDir   func(home string) string
	ProjectDir  string
	Detect      func(home string) bool
}

var supportedAgents = []Agent{
	{
		ID:          "claude-code",
		DisplayName: "Claude Code",
		Aliases:     []string{"claude", "claudecode"},
		GlobalDir: func(home string) string {
			if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
				return filepath.Join(dir, "skills")
			}
			return filepath.Join(home, ".claude", "skills")
		},
		ProjectDir: filepath.Join(".claude", "skills"),
		Detect: func(home string) bool {
			if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
				return true
			}
			_, err := os.Stat(filepath.Join(home, ".claude"))
			return err == nil
		},
	},
	{
		ID:          "antigravity-cli",
		DisplayName: "Antigravity CLI (AGY)",
		Aliases:     []string{"agy", "antigravity-cli"},
		GlobalDir: func(home string) string {
			return filepath.Join(home, ".gemini", "antigravity-cli", "skills")
		},
		ProjectDir: filepath.Join(".agents", "skills"),
		Detect: func(home string) bool {
			if os.Getenv("ANTIGRAVITY_AGENT") != "" {
				return true
			}
			_, err := os.Stat(filepath.Join(home, ".gemini", "antigravity-cli"))
			return err == nil
		},
	},
	{
		ID:          "antigravity",
		DisplayName: "Antigravity",
		Aliases:     []string{"antigravity-ide"},
		GlobalDir: func(home string) string {
			return filepath.Join(home, ".gemini", "antigravity", "skills")
		},
		ProjectDir: filepath.Join(".agents", "skills"),
		Detect: func(home string) bool {
			_, err := os.Stat(filepath.Join(home, ".gemini", "antigravity"))
			return err == nil
		},
	},
	{
		ID:          "codex",
		DisplayName: "Codex",
		Aliases:     []string{"codex-cli"},
		GlobalDir: func(home string) string {
			if dir := os.Getenv("CODEX_HOME"); dir != "" {
				return filepath.Join(dir, "skills")
			}
			return filepath.Join(home, ".codex", "skills")
		},
		ProjectDir: filepath.Join(".agents", "skills"),
		Detect: func(home string) bool {
			if os.Getenv("CODEX_HOME") != "" {
				return true
			}
			if _, err := os.Stat(filepath.Join(home, ".codex")); err == nil {
				return true
			}
			if _, err := os.Stat("/etc/codex"); err == nil {
				return true
			}
			return false
		},
	},
	{
		ID:          "opencode",
		DisplayName: "OpenCode",
		Aliases:     []string{"open-code"},
		GlobalDir: func(home string) string {
			if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
				return filepath.Join(xdg, "opencode", "skills")
			}
			return filepath.Join(home, ".config", "opencode", "skills")
		},
		ProjectDir: filepath.Join(".agents", "skills"),
		Detect: func(home string) bool {
			if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
				if _, err := os.Stat(filepath.Join(xdg, "opencode")); err == nil {
					return true
				}
			}
			_, err := os.Stat(filepath.Join(home, ".config", "opencode"))
			return err == nil
		},
	},
	{
		ID:          "cursor",
		DisplayName: "Cursor",
		Aliases:     []string{},
		GlobalDir: func(home string) string {
			return filepath.Join(home, ".cursor", "skills")
		},
		ProjectDir: filepath.Join(".agents", "skills"),
		Detect: func(home string) bool {
			_, err := os.Stat(filepath.Join(home, ".cursor"))
			return err == nil
		},
	},
	{
		ID:          "gemini-cli",
		DisplayName: "Gemini CLI",
		Aliases:     []string{"gemini"},
		GlobalDir: func(home string) string {
			return filepath.Join(home, ".gemini", "skills")
		},
		ProjectDir: filepath.Join(".agents", "skills"),
		Detect: func(home string) bool {
			_, err := os.Stat(filepath.Join(home, ".gemini"))
			return err == nil
		},
	},
}

type resolvedTarget struct {
	Agent string
	Path  string
}

func resolveTargets(home, cwd, explicitDir, agentFlag string, allFlag, projectFlag bool) ([]resolvedTarget, error) {
	if explicitDir != "" {
		dest := filepath.Join(expandUser(explicitDir), skill.Name)
		return []resolvedTarget{{Agent: "custom", Path: dest}}, nil
	}

	var selected []Agent
	if allFlag || agentFlag == "*" || agentFlag == "all" {
		selected = append(selected, supportedAgents...)
	} else if agentFlag != "" {
		names := strings.Split(agentFlag, ",")
		for _, raw := range names {
			name := strings.TrimSpace(strings.ToLower(raw))
			if name == "" {
				continue
			}
			found := false
			for _, a := range supportedAgents {
				if strings.ToLower(a.ID) == name {
					selected = append(selected, a)
					found = true
					break
				}
				for _, alias := range a.Aliases {
					if strings.ToLower(alias) == name {
						selected = append(selected, a)
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				valid := make([]string, 0, len(supportedAgents))
				for _, a := range supportedAgents {
					valid = append(valid, a.ID)
				}
				return nil, exitcode.New(exitcode.Usage, "unknown agent %q: supported agents are %s", raw, strings.Join(valid, ", "))
			}
		}
	} else {
		// Auto-detect installed agents
		for _, a := range supportedAgents {
			if a.Detect(home) {
				selected = append(selected, a)
			}
		}
		if len(selected) == 0 {
			// Fallback to claude-code for backwards compatibility
			selected = append(selected, supportedAgents[0])
		}
	}

	// Deduplicate by destination path
	destToAgents := make(map[string][]string)
	order := make([]string, 0)

	for _, a := range selected {
		var base string
		if projectFlag {
			base = filepath.Join(cwd, a.ProjectDir)
		} else {
			base = a.GlobalDir(home)
		}
		dest := filepath.Join(base, skill.Name)
		if len(destToAgents[dest]) == 0 {
			order = append(order, dest)
		}
		destToAgents[dest] = append(destToAgents[dest], a.ID)
	}

	targets := make([]resolvedTarget, 0, len(order))
	for _, dest := range order {
		agents := destToAgents[dest]
		targets = append(targets, resolvedTarget{
			Agent: strings.Join(agents, ", "),
			Path:  dest,
		})
	}
	return targets, nil
}

func newSkillCommand(f *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install the Agent Skill for AI coding agents",
		Long: "Writes an Agent Skill so Claude Code, Antigravity (AGY), Codex, and similar agents can drive npmctl safely.\n\n" +
			"The skill pre-approves read commands and --dry-run only. No write command is\n" +
			"pre-approved, so every mutation re-enters the agent's normal permission flow.",
	}
	cmd.AddCommand(newSkillInstallCommand(f))
	return cmd
}

func newSkillInstallCommand(f *globalFlags) *cobra.Command {
	var dir, agentsMD, agentFlag string
	var allFlag, projectFlag, withDocs bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write the skill folder (idempotent; never overwrites local edits)",
		Long: "Writes the Agent Skill to AI coding agent directories.\n\n" +
			"By default, npmctl scans for installed agents (Claude Code, Antigravity CLI, Codex, OpenCode,\n" +
			"Cursor, Gemini CLI) and installs to all detected agents globally. Use --agent to target specific\n" +
			"agents, --all for all supported agents, --project for project-level installation (.agents/skills),\n" +
			"or --dir to specify an exact custom directory.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := newRuntime(f)
			if err != nil {
				return err
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			targets, err := resolveTargets(home, cwd, dir, agentFlag, allFlag, projectFlag)
			if err != nil {
				return err
			}

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

			type targetResult struct {
				Agent string         `json:"agent"`
				Path  string         `json:"path"`
				Files []skill.Result `json:"files"`
			}

			var targetResults []targetResult
			totalPreserved := 0

			for _, target := range targets {
				manifest, err := skill.LoadManifest(target.Path)
				if err != nil {
					return err
				}
				results, err := installFiles(target.Path, payload, manifest, f.dryRun)
				if err != nil {
					return err
				}
				totalPreserved += countPreserved(results)
				targetResults = append(targetResults, targetResult{
					Agent: target.Agent,
					Path:  target.Path,
					Files: results,
				})
			}

			out := map[string]any{
				"skill":   skill.Name,
				"targets": targetResults,
				"dry_run": f.dryRun,
			}
			if len(targetResults) == 1 {
				out["agent"] = targetResults[0].Agent
				out["path"] = targetResults[0].Path
				out["files"] = targetResults[0].Files
			} else {
				var allFiles []skill.Result
				for _, tr := range targetResults {
					allFiles = append(allFiles, tr.Files...)
				}
				out["files"] = allFiles
			}

			if agentsMD != "" {
				note, err := updateAgentsFile(rt, agentsMD, f.dryRun, f.force)
				if err != nil {
					return err
				}
				out["agents_md"] = note
			}
			if totalPreserved > 0 {
				fmt.Fprintf(rt.stderr,
					"%d file(s) had local edits and were left untouched; delete them to accept the shipped version\n",
					totalPreserved)
			}
			return output.Render(rt.stdout, rt.format, out)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "explicit skills directory (overrides agent auto-detection)")
	cmd.Flags().StringVarP(&agentFlag, "agent", "a", "", "specify agent(s) to install to (e.g. 'claude,agy,codex', '*' for all)")
	cmd.Flags().BoolVar(&allFlag, "all", false, "install to all supported agents")
	cmd.Flags().BoolVar(&projectFlag, "project", false, "install to project-level skills directory instead of global")
	cmd.Flags().StringVar(&agentsMD, "agents-md", "", "also append a pointer to this AGENTS.md (or GEMINI.md)")
	cmd.Flags().BoolVar(&withDocs, "with-command-reference", true, "generate references/command-reference.md")
	return cmd
}

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
