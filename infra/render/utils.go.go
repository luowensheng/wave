package render

import (
	"bytes"
	"easyserver/infra/format"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

func RenderToString(text string, data any) (string, error) {
	buffer, err := Render(text, data)
	if err != nil {
		return "", err
	}
	return buffer.String(), nil
}

func marshal(v any) (string, error) {
	w := strings.Builder{}
	enc := json.NewEncoder(&w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", strings.Repeat(" ", 4))
	err := enc.Encode(v)
	if err != nil {
		return "", err
	}
	content := w.String()
	return content, nil
}

func Render(text string, data any, fn ...template.FuncMap) (*bytes.Buffer, error) {
	text = strings.TrimSpace(text)
	var bodyBuf bytes.Buffer

	if text == "" {
		return &bodyBuf, nil
	}
	funcMap := template.FuncMap{
		"escape": func(s string) string {
			b, _ := marshal(s)
			return b
		},
		"urlPathEscape": url.PathEscape,		
		"urlQueryEscape": url.QueryEscape,
		"toJSON": func(v any) string {
			b, _ := marshal(v)
			return b
		},
		"renderToHTML": renderToHTML,
		"renderTemplate": func(path string, content any) string {
			templateBytes, err := os.ReadFile(path)
			if err != nil {
				return err.Error()
			}
			tmpl, err := template.New("body").Funcs(template.FuncMap{
				"escape": func(s string) string {
					b, _ := marshal(s)
					return b
				},
				"toJSON": func(v any) string {
					b, _ := marshal(v)
					return b
				},
			}).Parse(string(templateBytes))
			if err != nil {
				return err.Error()
			}

			w := strings.Builder{}

			if err := tmpl.Execute(&w, content); err != nil {
				// http.Error(w, "Template execution error", http.StatusInternalServerError)
				// log.Printf("Template execution error: %v", err)
				return err.Error()
			}
			return w.String()
		},
	}

	dataMap, ok := data.(map[string]any)
	if ok {
		for key, value := range dataMap {
			funcMap[key] = func() string {
				content, err := marshal(value)
				if err != nil {
					panic(err.Error())
				}
				return content
			}
		}
	}

	// Parse and execute body template
	tmpl, err := template.New("body").Funcs(funcMap).Parse(text)
	if err != nil {
		return nil, err
	}

	tmpl.Option("missingkey=error")

	if err := tmpl.Execute(&bodyBuf, data); err != nil {
		// http.Error(w, "Template execution error", http.StatusInternalServerError)
		// log.Printf("Template execution error: %v", err)
		return nil, err
	}

	fmt.Printf("==========RENDERED CONTENT START============\n%s,\n\n\n%s\n==========RENDERED CONTENT END============\n", text, bodyBuf.String())

	return &bodyBuf, nil

}

// renderDirectoryIndex returns a Markdown-formatted directory listing.
func RenderDirectoryIndex(basePath string, entries []os.DirEntry, relPath string, ignorePatterns []string) string {
	var sb strings.Builder

	// Title
	header := fmt.Sprintf("[Home](/)\n\n[Back](%s)\n\n## Index of %s\n\n", filepath.Dir(relPath), relPath)

	// Table header
	sb.WriteString("| Icon | Name | Size |\n")
	sb.WriteString("|------|------|------|\n")

	for _, entry := range entries {
		name := entry.Name()
		entryRelPath := filepath.Join(relPath, name)

		// Skip ignored entries
		shouldIgnore := false
		for _, pattern := range ignorePatterns {
			if strings.Contains(entryRelPath, pattern) {
				shouldIgnore = true
				break
			}
		}
		if shouldIgnore {
			continue
		}

		// Determine icon and link
		var icon, displayName, href string
		if entry.IsDir() {
			icon = "📁"
			href = url.PathEscape(name) + "/"
			displayName = name + "/"
		} else {
			icon = "📄"
			href = url.PathEscape(name)
			displayName = name
		}

		// Escape markdown special characters in displayName
		escapedName := strings.ReplaceAll(displayName, "|", "\\|")
		escapedName = strings.ReplaceAll(escapedName, "[", "\\[")
		escapedName = strings.ReplaceAll(escapedName, "]", "\\]")
		escapedName = strings.ReplaceAll(escapedName, "`", "\\`")

		// Format link
		link := fmt.Sprintf("[%s](%s)", escapedName, href)

		// Get file size if it's a file
		size := ""
		if !entry.IsDir() {
			if info, err := entry.Info(); err == nil {

				size = format.HumanizeBytes(info.Size())
			} else {
				size = "–"
			}
		} else {
			size = "–"
		}

		// Write table row
		sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", icon, link, size))
	}

	generator := NewHTMLGenerator()
	generator.Title = relPath
	generator.DontEnhanceURLs = true
	generator.IsFile = true
	info, _ := os.Stat(basePath)
	metadata := fmt.Sprintf("| Property | Value |\n|---|---|\n| Modified | %s |",
		info.ModTime().Format("2006/01/02 15:04:05"),
	)
	result := generator.GenerateHTML(fmt.Sprintf("\n%s\n%s\n---\n%s", header, metadata, sb.String()))
	return strings.ReplaceAll(result, `target="_blank"`, "")

}
