// Package skill installs the Agent Skill that lets Claude Code, Codex and similar
// agents drive npmctl.
package skill

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
)

// Name is the skill's directory name. The spec requires the frontmatter `name` to match
// it, so both are derived from this constant.
const Name = "nginx-proxy-manager"

//go:embed files/SKILL.md files/references/*.md
var files embed.FS

const root = "files"

// File is one file to install.
type File struct {
	// RelPath is relative to the skill directory, e.g. "references/common-tasks.md".
	RelPath string
	Content []byte
}

// Files returns the embedded skill payload, sorted for stable output.
func Files() ([]File, error) {
	var out []File
	err := fs.WalkDir(files, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		content, err := files.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, File{RelPath: filepath.ToSlash(rel), Content: content})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read embedded skill files: %w", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}

// SkillMarkdown returns the SKILL.md content, for tests that assert on its frontmatter.
func SkillMarkdown() ([]byte, error) {
	return files.ReadFile(root + "/SKILL.md")
}
