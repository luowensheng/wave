package common

import (
	"encoding/json"
	"fmt"
	"text/template"
	"regexp"
	"strings"
)

var pARAM_RGX = regexp.MustCompile(`\$[\w_]+`)

func RemoveAdjacentQuotes(content string) string {
	return strings.ReplaceAll(content, `""`, `"`)
}

func EscapeNewLine(content string) string {
	return strings.ReplaceAll(content, "\n", "\\\n")
}

func RenderTemplateWithQuotes(content string, vars map[string]string) string {

	keys := map[string]bool{}
	for k := range vars {
		keys[k] = true
	}
	for _, param := range pARAM_RGX.FindAllString(content, -1) {
		if !keys[param] {
			continue
		}
		content = strings.ReplaceAll(content, param, vars[param])
	}
	return content
}

func JSONSprint(v any) string {
	builder := strings.Builder{}
	enc := json.NewEncoder(&builder)
	enc.SetEscapeHTML(false)
	enc.Encode(v)

	return builder.String()
}

func RenderTemplate(content string, vars map[string]string) string {

	for _, param := range pARAM_RGX.FindAllString(content, -1) {
		if vars[param] == "" {
			continue
		}
		content = strings.ReplaceAll(content, fmt.Sprintf(`"%s"`, param), vars[param])
	}
	return content
}

func RenderTemplateGO(text string, vars any, funcMaps ...template.FuncMap) (string, error) {
	funcMap := template.FuncMap{}
	if len(funcMaps) > 0 {
		funcMap = funcMaps[0]
	}

	templ := template.Must(template.New("").Funcs(funcMap).Parse(text))
	builder := strings.Builder{}
	err := templ.Execute(&builder, vars)
	if err != nil {
		return "", err
	}
	return builder.String(), nil
}
