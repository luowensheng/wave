package bundler

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Config struct {
	Enabled       bool         `yaml:"enabled"`
	DistDir       string       `yaml:"dist_dir"`
	BundleName    string       `yaml:"bundle_name"`
	CacheBust     bool         `yaml:"cache_bust"`
	JSFiles       []string     `yaml:"js_files"`
	Templates     []Template   `yaml:"templates"`
	Dependencies  []Dependency `yaml:"dependencies"`
	IndexTemplate string       `yaml:"index_template"`

	// Watch mode: re-run the build when source files change.
	Watch           bool `yaml:"watch"`
	WatchDebounceMs int  `yaml:"watch_debounce_ms"` // default 300
}

type Template struct {
	Pattern   string `yaml:"pattern"`
	IDRegex   string `yaml:"id_regex"`
	IDReplace string `yaml:"id_replacement"`
}

type Dependency struct {
	Src  string `yaml:"src"`
	Dest string `yaml:"dest"`
}

// Run executes the build. Safe to call multiple times.
func Run(cfg Config) error {
	if !cfg.Enabled {
		return nil
	}

	dist := cfg.DistDir
	if dist == "" {
		dist = "dist"
	}
	if err := os.MkdirAll(dist, 0755); err != nil {
		return fmt.Errorf("create dist dir: %w", err)
	}

	jsPaths, err := resolveGlobs(cfg.JSFiles)
	if err != nil {
		return fmt.Errorf("resolve js files: %w", err)
	}
	if len(jsPaths) == 0 {
		return fmt.Errorf("no js files matched patterns")
	}

	templateBlocks, err := resolveTemplates(cfg.Templates)
	if err != nil {
		return fmt.Errorf("resolve templates: %w", err)
	}

	depRefs := []string{}
	for _, dep := range cfg.Dependencies {
		destPath := filepath.Join(dist, dep.Dest)
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("create dep dir: %w", err)
		}
		if err := copyFile(dep.Src, destPath); err != nil {
			return fmt.Errorf("copy dependency %s: %w", dep.Src, err)
		}
		depRefs = append(depRefs, "/"+dep.Dest)
	}

	var bundle strings.Builder

	if len(templateBlocks) > 0 {
		bundle.WriteString("// === INLINED TEMPLATES ===\n")
		for _, t := range templateBlocks {
			bundle.WriteString(t)
			bundle.WriteString("\n")
		}
		bundle.WriteString("\n")
	}

	bundle.WriteString("// === APPLICATION CODE ===\n")
	for _, p := range jsPaths {
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("read js %s: %w", p, err)
		}
		bundle.Write(data)
		bundle.WriteString("\n")
	}

	bundled := minifyJS(bundle.String())

	version := ""
	if cfg.CacheBust {
		h := sha256.Sum256([]byte(bundled))
		version = fmt.Sprintf("?v=%x", h[:8])
	}

	bundlePath := filepath.Join(dist, cfg.BundleName)
	if err := os.WriteFile(bundlePath, []byte(bundled), 0644); err != nil {
		return fmt.Errorf("write bundle: %w", err)
	}

	if err := generateIndex(cfg, dist, cfg.BundleName, version, depRefs); err != nil {
		return fmt.Errorf("generate index.html: %w", err)
	}

	fmt.Printf("[BUNDLE] Built %s (%d JS files, %d templates, %d deps) -> %s\n",
		cfg.BundleName, len(jsPaths), len(templateBlocks), len(cfg.Dependencies), bundlePath)
	return nil
}

func generateIndex(cfg Config, distDir, bundleName, version string, deps []string) error {
	indexPath := filepath.Join(distDir, "index.html")

	if cfg.IndexTemplate != "" {
		content, err := os.ReadFile(cfg.IndexTemplate)
		if err != nil {
			return fmt.Errorf("read index template: %w", err)
		}
		bundleTag := fmt.Sprintf(`<script src="/%s%s"></script>`, bundleName, version)
		output := strings.Replace(string(content), "</body>", bundleTag+"\n</body>", 1)
		return os.WriteFile(indexPath, []byte(output), 0644)
	}

	minimal := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>App</title>
<link rel="stylesheet" href="/style.css">
%s</head>
<body>
<div id="app"></div>
<script src="/%s%s"></script>
</body>
</html>`,
		generateDepTags(deps),
		bundleName,
		version,
	)
	return os.WriteFile(indexPath, []byte(minimal), 0644)
}

func generateDepTags(deps []string) string {
	var sb strings.Builder
	for _, d := range deps {
		sb.WriteString(fmt.Sprintf(`<script src="%s"></script>`+"\n", d))
	}
	return sb.String()
}

func resolveGlobs(patterns []string) ([]string, error) {
	var files []string
	seen := make(map[string]bool)

	for _, p := range patterns {
		if !strings.Contains(p, "*") {
			if !seen[p] {
				files = append(files, p)
				seen[p] = true
			}
			continue
		}

		if strings.Contains(p, "**") {
			parts := strings.SplitN(p, "**", 2)
			base := parts[0]
			suffix := strings.TrimPrefix(parts[1], string(filepath.Separator))
			err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				rel, _ := filepath.Rel(base, path)
				match, _ := filepath.Match(suffix, rel)
				if match && !seen[path] {
					files = append(files, path)
					seen[path] = true
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			continue
		}

		matches, err := filepath.Glob(p)
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			if !seen[m] {
				files = append(files, m)
				seen[m] = true
			}
		}
	}
	return files, nil
}

func resolveTemplates(templates []Template) ([]string, error) {
	var blocks []string
	for _, t := range templates {
		matches, err := resolveGlobs([]string{t.Pattern})
		if err != nil {
			return nil, err
		}
		re, err := regexp.Compile(t.IDRegex)
		if err != nil {
			return nil, fmt.Errorf("invalid id_regex '%s': %w", t.IDRegex, err)
		}
		for _, file := range matches {
			data, err := os.ReadFile(file)
			if err != nil {
				return nil, err
			}
			// Use the path after "static/" for ID generation, fall back to full path
			rel := file
			if idx := strings.Index(file, "static/"); idx >= 0 {
				rel = file[idx+len("static/"):]
			}
			id := re.ReplaceAllString(rel, t.IDReplace)
			id = strings.ToLower(id)
			html := strings.TrimSpace(string(data))
			block := fmt.Sprintf(`<script type="text/x-template" id="%s">%s</script>`, id, html)
			blocks = append(blocks, block)
		}
	}
	return blocks, nil
}

func minifyJS(src string) string {
	reBlock := regexp.MustCompile(`/\*[\s\S]*?\*/`)
	src = reBlock.ReplaceAllString(src, "")
	reLine := regexp.MustCompile(`//[^\n]*`)
	src = reLine.ReplaceAllString(src, "")
	reWS := regexp.MustCompile(`[ \t]+`)
	src = reWS.ReplaceAllString(src, " ")
	reNewlines := regexp.MustCompile(`\n\s*\n+`)
	src = reNewlines.ReplaceAllString(src, "\n")
	return strings.TrimSpace(src)
}

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0644)
}
