package render

import (
	"bytes"
	"easyserver/infra/format"
	"encoding/base64"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"unicode"
	"unicode/utf8"

	"github.com/russross/blackfriday/v2"
)

// HTMLGenerator handles conversion of strings to clean HTML
type HTMLGenerator struct {
	prismImports    string
	Title           string
	DontEnhanceURLs bool
	IsFile          bool
}

// NewHTMLGenerator creates a new HTML generator instance
func NewHTMLGenerator() *HTMLGenerator {
	return &HTMLGenerator{
		prismImports: getPrismImports(),
	}
}

// getPrismImports returns the Prism.js CSS and JavaScript imports
func getPrismImports() string {
	return `
        <link href="https://cdnjs.cloudflare.com/ajax/libs/prism/1.29.0/themes/prism-tomorrow.min.css" rel="stylesheet" />
        <script src="https://cdnjs.cloudflare.com/ajax/libs/prism/1.29.0/components/prism-core.min.js"></script>
        <script src="https://cdnjs.cloudflare.com/ajax/libs/prism/1.29.0/plugins/autoloader/prism-autoloader.min.js"></script>
    `
}

// isURL checks if the string is a valid HTTP/HTTPS URL
func (h *HTMLGenerator) isURL(str string) bool {
	// Remove HTML tags and trim
	plainText := h.stripHTMLTags(str)
	plainText = strings.TrimSpace(plainText)

	// Parse URL
	parsedURL, err := url.Parse(plainText)
	if err != nil {
		return false
	}

	// Check if it's HTTP or HTTPS
	return parsedURL.Scheme == "http" || parsedURL.Scheme == "https"
}

func (h *HTMLGenerator) isB64(s string) bool {
	if s == "" {
		return false
	}
	// Basic pattern: data:<MIME-type>;base64,<data>
	re := regexp.MustCompile(`^data:([a-zA-Z0-9]+/[a-zA-Z0-9\-\+\.]+);base64,([A-Za-z0-9+/=]+)$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return false
	}

	// Extract the Base64 part
	b64Data := matches[2]
	_, err := base64.StdEncoding.DecodeString(b64Data)
	return err == nil
}

// isURL checks if the string is a valid HTTP/HTTPS URL
func (h *HTMLGenerator) isURLList(str string) bool {
	for text := range strings.SplitSeq(str, "\n") {
		// Remove HTML tags and trim
		plainText := h.stripHTMLTags(strings.TrimSpace(text))
		plainText = strings.TrimSpace(plainText)

		// Parse URL
		parsedURL, err := url.Parse(plainText)
		if err != nil {
			return false
		}

		// Check if it's HTTP or HTTPS
		if parsedURL.Scheme == "http" || parsedURL.Scheme == "https" {
			return true
		}
	}

	return false

}

// stripHTMLTags removes HTML tags from a string
func (h *HTMLGenerator) stripHTMLTags(str string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	return re.ReplaceAllString(str, "")
}

// containsURL checks if string contains any URLs
func (h *HTMLGenerator) containsURL(str string) bool {
	plainText := h.stripHTMLTags(str)
	urlPattern := regexp.MustCompile(`https?://(?:www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b(?:[-a-zA-Z0-9()@:%_\+.~#?&//=]*)`)
	return urlPattern.MatchString(plainText)
}

// isMarkdownText determines if the text is markdown (not HTML)
func (h *HTMLGenerator) isMarkdownText(text string) bool {
	trimmed := strings.TrimSpace(text)
	return !strings.HasPrefix(trimmed, "<")
}

// GenerateHTML converts input string to clean HTML
func (h *HTMLGenerator) GenerateHTML(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return h.wrapInHTMLDoc("No content provided", `<p style="color: #999; font-style: italic;">No content provided...</p>`)
	}

	// Escape quotes for safety
	// escapedInput := html.EscapeString(input)
	// escapedInput = strings.ReplaceAll(escapedInput, `"`, `&quot;`)

	isMarkdown := h.isMarkdownText(input)

	var htmlContent string

	if isMarkdown {
		if h.isURL(input) {
			// If it's a URL, create an embed object
			return wrapURL(input)
			// htmlContent = fmt.Sprintf(`<object data="%s" style="width:100vw; height:100vh;"></object>`, html.EscapeString(trimmedURL))
			// return h.addDefaultStyles(htmlContent)

		} else if h.isB64(input) {
			return wrapB64(input)
		} else {
			// Convert markdown to HTML
			htmlContent = h.markdownToHTML(input)
		}

		title := h.Title
		if title == "" {
			title = ExtractCleanTitle(input)
		}
		return h.wrapInHTMLDoc(title, htmlContent)
	} else {
		// It's already HTML, just add default styles if needed
		htmlContent = input
		if !strings.Contains(htmlContent, "<style>") {
			htmlContent = h.addDefaultStyles(htmlContent)
		}
		return htmlContent
	}
}

// File extension sets for different content types
var (
	documentExts = map[string]bool{
		".doc": true, ".docx": true, ".dotx": true, ".dotm": true, ".docm": true,
		".odt": true, ".xls": true, ".xlsx": true, ".xlsm": true, ".xlsb": true,
		".ods": true, ".ppt": true, ".pptx": true, ".pps": true, ".ppsx": true,
		".pot": true, ".potx": true, ".pptm": true, ".potm": true, ".ppam": true,
		".ppsm": true, ".odp": true, ".pdf": true, ".txt": true, ".csv": true,
		".rtf": true,
		// ".pages": true, ".numbers": true, ".key": true,
	}

	imageExts = map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true,
		".svg": true, ".webp": true, ".tiff": true, ".tif": true, ".ico": true,
		".heic": true, ".heif": true, ".avif": true, ".jfif": true, ".pjpeg": true,
		".pjp": true, ".apng": true,
	}

	videoExts = map[string]bool{
		".mp4": true, ".avi": true, ".mov": true, ".wmv": true, ".flv": true,
		".mkv": true, ".webm": true, ".m4v": true, ".mpg": true, ".mpeg": true,
		".3gp": true, ".3g2": true, ".ogv": true, ".ts": true, ".m2ts": true,
		".mts": true, ".vob": true, ".f4v": true, ".asf": true, ".rm": true,
		".rmvb": true, ".divx": true,
	}

	audioExts = map[string]bool{
		".mp3": true, ".wav": true, ".ogg": true, ".m4a": true, ".aac": true,
		".flac": true, ".wma": true, ".aiff": true, ".ape": true, ".opus": true,
		".alac": true, ".mid": true, ".midi": true, ".amr": true, ".ac3": true,
		".dts": true, ".ra": true, ".tta": true, ".wv": true, ".mka": true,
	}
)

const htmlTemplatePart = `
{{if eq .Type "document"}}
        <iframe src="https://view.officeapps.live.com/op/embed.aspx?src={{.URL}}"
		allowfullscreen
  		allow="fullscreen"
		loading="lazy"></iframe>
{{else if eq .Type "image"}}
	<img loading="lazy" src="{{.URL}}" alt="{{.URL}}">
{{else if eq .Type "video"}}
	<video width="1000" src="{{.URL}}" controls autoplay playsinline loading="lazy"></video>
{{else if eq .Type "audio"}}
	<audio src="{{.URL}}" controls autoplay></audio>
{{else}}
	<!-- <iframe loading="lazy" src="{{.URL}}" width="1000" 
	allowfullscreen
  allow="fullscreen"
	style="min-width:1000px;max-width:1000px; width:100%; min-height:1200px; height:fit-content; aspect-ratio:auto; display:block; border:none;"
	>
		</iframe> -->
	<a href="{{.URL}}" target="_blank">{{.URL}}</a>
{{end}}
`

// <a href="{{.URL}}" target="_blank">{{.URL}}</a>
// display: flex;
// flex-direction: column;
// justify-content: center;
const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no, viewport-fit=cover">
    <meta name="apple-mobile-web-app-capable" content="yes">
    <meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">
    <meta name="mobile-web-app-capable" content="yes">
    <title>{{.URL}}</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        html, body {
            margin: 0;
            padding: 0;
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Oxygen', 'Ubuntu', 'Cantarell', sans-serif;
            overflow: hidden;
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
        }
        
        body { 
            width: 100dvw;
            height: 100dvh;
            width: 100vw;
            height: 100vh;

            align-items: center;
            -webkit-user-select: none;
            -moz-user-select: none;
            -ms-user-select: none;
            user-select: none;
            -webkit-touch-callout: none;
            background: #000;
        }
        
        .fullscreen-content {
            width: 100%;
            height: 100%;
            border: none;
            padding: env(safe-area-inset-top) env(safe-area-inset-right) env(safe-area-inset-bottom) env(safe-area-inset-left);
        }
        
        img.fullscreen-content {
            object-fit: contain;
        }
        
        video.fullscreen-content {
            object-fit: contain;
        }
        
        iframe.fullscreen-content {
            background: white;
        }
        
        .error-message {
            color: white;
            text-align: center;
            padding: 20px;
        }
        
        .error-message a {
            color: #4A9EFF;
            text-decoration: none;
        }
        
        input, select, textarea {
            font-size: 16px !important;
        }
    </style>
</head>
<body>
    {{if eq .Type "document"}}
        <iframe 
		 allowfullscreen
 		allow="fullscreen"
		loading="lazy"
  
  src="https://view.officeapps.live.com/op/embed.aspx?src={{.URL}}" class="fullscreen-content"></iframe>
    {{else if eq .Type "image"}}
        <img loading="lazy" src="{{.URL}}" class="fullscreen-content" alt="Image">
    {{else if eq .Type "video"}}
        <video width="1000" src="{{.URL}}" class="fullscreen-content" controls autoplay playsinline></video>
    {{else if eq .Type "audio"}}
        <audio width="1000" src="{{.URL}}" class="fullscreen-content" controls autoplay></audio>
    {{else}}
       <!-- <iframe width="1000" src="{{.URL}}" class="fullscreen-content"
		allowfullscreen
 		allow="fullscreen"
		loading="lazy"
		>
            <div class="error-message">
                <p>Content cannot be displayed</p>
                <p><a href="{{.URL}}">Open directly: {{.URL}}</a></p>
            </div>
        </iframe> -->

		<a href="{{.URL}}" target="_blank">{{.URL}}</a>
    {{end}}

    
    <script>
        let lastTouchEnd = 0;
        document.addEventListener('touchend', function (event) {
            const now = (new Date()).getTime();
            if (now - lastTouchEnd <= 300) {
                event.preventDefault();
            }
            lastTouchEnd = now;
        }, false);
        
        if (screen.orientation && screen.orientation.lock) {
            screen.orientation.lock('portrait').catch(() => {
                console.log('Orientation lock not supported');
            });
        }
        
        function handleViewportChange() {
            document.body.style.height = '100dvh';
        }
        
        window.addEventListener('orientationchange', handleViewportChange);
        window.addEventListener('resize', handleViewportChange);
    </script>
</body>
</html>`

// body {
//     margin: 0;
//     font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Oxygen', 'Ubuntu', 'Cantarell', sans-serif;
//     line-height: 1.6;
//     color: #333;
//     padding: 20px;
//     min-width: 800px;
// 	width: fit-content;
//     margin: 0 auto;
// }
// a {
//     color: #0366d6;
//     text-decoration: none;
// }
// a:hover {
//     text-decoration: underline;
// }
// audio {
//     display: block;
//     margin: 8px 0;
//     width: 100%;
// }

// pre {
//     background-color: #f6f8fa;
//     border-radius: 6px;
//     padding: 16px;
//     overflow-x: auto;
// }
// code {
//     background-color: #f6f8fa;
//     border-radius: 3px;
//     padding: 2px 4px;
//     font-size: 85%;
// }
// pre code {
//     background-color: transparent;
//     padding: 0;
// }

// pre img {
// 	margin: auto !important;
// }

// blockquote {
//     border-left: 4px solid #dfe2e5;
//     padding-left: 16px;
//     margin-left: 0;
//     color: #6a737d;
// }
// table {
//     border-collapse: collapse;
//     width: 100%;
//     margin: 16px 0;
// }
// table th, table td {
//     border: 1px solid #dfe2e5;
//     padding: 8px 12px;
//     text-align: left;
// }
// table th {
//     background-color: #f6f8fa;
//     font-weight: 600;
// }
// img {
//     max-width: 100%;
//     height: auto;
// }

func getContentType(extension string) string {
	if documentExts[extension] {
		return "document"
	}
	if imageExts[extension] {
		return "image"
	}
	if videoExts[extension] {
		return "video"
	}
	if audioExts[extension] {
		return "audio"
	}
	return "other"
}

func stringToHTML(url string) string {
	// Extract extension from URL (before query parameters)
	urlPath := strings.Split(url, "?")[0]
	extension := strings.ToLower(filepath.Ext(urlPath))

	// Determine content type
	contentType := getContentType(extension)

	// Parse template
	tmpl, err := template.New("html").Parse(htmlTemplatePart)
	if err != nil {
		return err.Error()
	}

	// Execute template
	var buf bytes.Buffer
	data := map[string]interface{}{
		"URL":  url,
		"Type": contentType,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return err.Error()
	}

	return strings.TrimSpace(buf.String())
}

func b64StringToHTML(url string) string {
	// Extract extension from URL (before query parameters)
	extension := "." + strings.Split(strings.Split(strings.SplitN(url, ";", 2)[0], ":")[1], "/")[1]
	// Determine content type
	contentType := getContentType(extension)

	// Parse template
	tmpl, err := template.New("html").Parse(htmlTemplate)
	if err != nil {
		return err.Error()
	}

	// Execute template
	var buf bytes.Buffer
	data := map[string]interface{}{
		"URL":  url,
		"Type": contentType,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return err.Error()
	}

	return buf.String()
}

func wrapB64(url string) string {
	// Extract extension from URL (before query parameters)
	extension := "." + strings.Split(strings.Split(strings.SplitN(url, ";", 2)[0], ":")[1], "/")[1]
	// Determine content type
	contentType := getContentType(extension)

	// Parse template
	tmpl, err := template.New("html").Parse(htmlTemplate)
	if err != nil {
		return err.Error()
	}

	// Execute template
	var buf bytes.Buffer
	data := map[string]interface{}{
		"URL":  url,
		"Type": contentType,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return err.Error()
	}

	return buf.String()
}

func wrapURL(url string) string {
	// Extract extension from URL (before query parameters)
	urlPath := strings.Split(url, "?")[0]
	extension := strings.ToLower(filepath.Ext(urlPath))

	// Determine content type
	contentType := getContentType(extension)

	// Parse template
	tmpl, err := template.New("html").Parse(htmlTemplate)
	if err != nil {
		return err.Error()
	}

	// Execute template
	var buf bytes.Buffer
	data := map[string]interface{}{
		"URL":  url,
		"Type": contentType,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return err.Error()
	}

	return buf.String()
}

// markdownToHTML converts markdown text to HTML
func (h *HTMLGenerator) markdownToHTML(markdown string) string {

	if !h.DontEnhanceURLs {
		lines := []string{}
		for line := range strings.SplitSeq(markdown, "\n") {
			trimmed := strings.TrimSpace(line)
			if h.isURL(trimmed) {
				lineHTML := stringToHTML(trimmed)
				// fmt.Println(lineHTML, "\n\n\n")
				lines = append(lines, lineHTML)
			} else if h.isB64(trimmed) {
				lineHTML := b64StringToHTML(trimmed)
				lines = append(lines, lineHTML)

			} else {
				lines = append(lines, line)
			}
		}

		markdown = strings.Join(lines, "\n")
	}

	// Configure blackfriday with common extensions
	extensions := blackfriday.CommonExtensions | blackfriday.AutoHeadingIDs | blackfriday.Footnotes

	// Convert markdown to HTML
	htmlBytes := blackfriday.Run([]byte(markdown), blackfriday.WithExtensions(extensions))
	htmlContent := string(htmlBytes)

	// Make links open in new tab
	linkRegex := regexp.MustCompile(`<a href=`)
	htmlContent = linkRegex.ReplaceAllString(htmlContent, `<a target="_blank" rel="noopener noreferrer" href=`)

	// Add controls to audio elements
	audioRegex := regexp.MustCompile(`<audio([^>]*)>([^<]*)</audio>`)
	htmlContent = audioRegex.ReplaceAllString(htmlContent, `<audio controls $1>$2</audio>`)

	return htmlContent
}

// display: flex;
// flex-direction: column;
// align-items: center;

// wrapInHTMLDoc wraps content in a complete HTML document
func (h *HTMLGenerator) wrapInHTMLDoc(title, content string) string {
	tmpl := `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <style>

        body {
            margin: 0;
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Oxygen', 'Ubuntu', 'Cantarell', sans-serif;
            line-height: 1.6;
            color: #333;
            padding: 20px;
            min-width: 800px;
            width: fit-content;
            margin: 0 auto;
            margin: auto;

        }

		* {
			max-width: 1200px;
		}

        a {
            color: #0366d6;
            text-decoration: none;
        }

        a:hover {
            text-decoration: underline;
        }

        audio {
            display: block;
            margin: 8px 0;
            width: 100%;
        }


        pre {
            background-color: #f6f8fa;
            border-radius: 6px;
            padding: 16px;
            overflow-x: auto;
        }

        code {
            background-color: #f6f8fa;
            border-radius: 3px;
            padding: 2px 4px;
            font-size: 85%;
        }

        pre code {
            background-color: transparent;
            padding: 0;
        }

        p:has(img) {
            display: flex;
            flex-direction: column;
            align-items: center;
            gap: 20px;
        }

		img {
            border-radius: 8px;
        }
        p img {

            max-height: 850px !important;
            border-radius: 8px;
        }

		/* Styles for screens 1000px tall or more */
		@media (min-height: 970px) {
			p img {
				max-height: 970px !important;
			}
		}

		@media (min-height: 1000px) {
			p img {
				max-height: 1000px !important;
			}
		}


        blockquote {
            border-left: 4px solid #dfe2e5;
            padding-left: 16px;
            margin-left: 0;
            color: #6a737d;
        }

        table {
            border-collapse: collapse;
            width: 100%;
            margin: 16px 0;
        }

        table th,
        table td {
            border: 1px solid #dfe2e5;
            padding: 8px 12px;
            text-align: left;
        }

        table th {
            background-color: #f6f8fa;
            font-weight: 600;
        }

        img {
            max-width: 100%;
            height: auto;
        }	

    </style>
    {{.PrismImports}}
</head>
<body>
    {{.Content}}
</body>
</html>`

	t, err := template.New("html").Parse(tmpl)
	if err != nil {
		return err.Error()
	}

	data := struct {
		Title        string
		PrismImports string
		Content      string
		IsFile       bool
	}{
		Title:        title,
		PrismImports: h.prismImports,
		Content:      content,
		IsFile:       h.IsFile,
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return err.Error()
	}

	return buf.String()
}

// addDefaultStyles adds default styles to existing HTML
func (h *HTMLGenerator) addDefaultStyles(htmlContent string) string {
	styleTag := `
    <style>
		html, body {
			margin: 0;
			padding: 0;
			font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Oxygen', 'Ubuntu', 'Cantarell', sans-serif
		}
        body { 
            width: 100vw;
            height: 100vh;
			width: 100dvw;
            height: 100dvh;
			display: flex;
			flex-direction: column;
			align-items: center;
			overflow-y: scroll;
        }

    </style>
`
	// justify-content: center;

	// Insert style before closing head tag
	headEndRegex := regexp.MustCompile(`(?i)</head>`)
	if headEndRegex.MatchString(htmlContent) {
		return headEndRegex.ReplaceAllString(htmlContent, styleTag+"</head>")
	}

	// If no head tag found, add it
	return fmt.Sprintf(`<!DOCTYPE html><html><head>%s</head><body>%s</body></html>`, styleTag, htmlContent)
}

// Example usage and testing
func testMD() {
	generator := NewHTMLGenerator()

	// Test cases
	testCases := []struct {
		name  string
		input string
	}{
		// {
		// 	name:  "Markdown Text",
		// 	input: "# Hello World\n\nThis is **bold** text with a [link](https://example.com).\n\n```go\nfunc main() {\n    fmt.Println(\"Hello\")\n}\n```",
		// },
		{
			// 			name: "URL",
			// 			input: `
			// 			<!DOCTYPE html>
			// <html lang="en">
			// <head>
			//   <meta charset="UTF-8" />
			//   <meta name="viewport" content="width=device-width, initial-scale=1.0"/>
			//   <title>Nutrition Crash Course</title>

			//   <!-- Inter font -->
			//   <link rel="preconnect" href="https://fonts.googleapis.com">
			//   <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
			//   <link href="https://fonts.googleapis.com/css2?family=Inter:wght@100..900&display=swap" rel="stylesheet">

			//   <style>
			//     :root{
			//       --bg:#ffffff;
			//       --text:#0f172a;       /* slate-900 */
			//       --muted:#475569;      /* slate-600 */
			//       --surface:#f8fafc;    /* slate-50 */

			//       /* section accents */
			//       --accent-alcohol:#e11d48; /* rose-600 */
			//       --accent-antiox:#16a34a;  /* green-600 */
			//       --accent-video:#2563eb;   /* blue-600 */

			//       /* highlight */
			//       --hl-bg:#fef9c3;      /* yellow-100 */
			//       --hl-text:#92400e;    /* amber-900 */
			//     }

			//     html { font-family: "Inter", ui-sans-serif, system-ui, -apple-system, "Segoe UI",
			//            Roboto, "Helvetica Neue", Arial, "Noto Sans", "Apple Color Emoji",
			//            "Segoe UI Emoji", "Segoe UI Symbol"; line-height: 1.6; }
			//     body { margin: 24px; color: var(--text); background: var(--bg); }
			//     h2 { font-weight: 800; margin: 0 0 .25rem; letter-spacing: -0.02em; }
			//     p  { color: var(--muted); max-width: 75ch; }

			//     /* Card sections with per-section accent */
			//     .card {
			//       margin: 1.25rem 0;
			//       padding: 1rem 1rem 1rem 1rem;
			//       border-radius: 12px;
			//       background: var(--surface);
			//       border: 1px solid #e2e8f0; /* slate-200 */
			//     }
			//     .card h3 { margin: 0 0 .5rem; font-weight: 700; }
			//     .card ul { margin: .25rem 0 0; padding-left: 1.25rem; }
			//     .card li { margin: .35rem 0; }

			//     /* Accent theming */
			//     .alcohol  { border-left: 6px solid var(--accent-alcohol); }
			//     .alcohol h3, .alcohol strong { color: var(--accent-alcohol); }

			//     .antiox   { border-left: 6px solid var(--accent-antiox); }
			//     .antiox h3, .antiox strong { color: var(--accent-antiox); }

			//     .video    { border-left: 6px solid var(--accent-video); }
			//     .video h3 { color: var(--accent-video); }

			//     /* Optional inline highlight for extra-important phrases */
			//     mark.hl {
			//       background: var(--hl-bg);
			//       color: var(--hl-text);
			//       padding: 0 .2em;
			//       border-radius: .25rem;
			//     }

			//     /* Responsive video embed */
			//     .video-embed {
			//       position: relative;
			//       width: 100%;
			//       max-width: 880px;
			//       aspect-ratio: 16 / 9;
			//       border-radius: 12px;
			//       overflow: hidden;
			//       border: 1px solid #e2e8f0;
			//       background: #000;
			//       margin-top: .5rem;
			//     }
			//     .video-embed iframe {
			//       position: absolute; inset: 0; width: 100%; height: 100%; border: 0;
			//     }
			//   </style>
			// </head>
			// <body>
			//   <h2>SCI 101 Nutrition Crash Course</h2>
			//   <p>The video titled <em>"SCI 101 Nutrition Crash Course"</em> by Coopersmith Career Consulting does not specifically mention the benefits of alcohol consumption or antioxidants in relation to homeostasis. However, general information is provided below.</p>

			//   <section class="card alcohol">
			//     <h3>Benefits of Alcohol Consumption</h3>
			//     <ul>
			//       <li><strong>Cardiovascular Health:</strong> <span>Moderate alcohol intake</span> may reduce the risk of heart disease.</li>
			//       <li><strong>Social Interaction:</strong> Alcohol can facilitate social interactions, contributing to mental well-being.</li>
			//       <li><strong>Potential Longevity:</strong> Moderate drinkers may have a lower risk of mortality compared to heavy drinkers or non-drinkers.</li>
			//     </ul>
			//     <!-- Optional extra emphasis -->
			//     <p style="margin:.5rem 0 0;color:#7c2d12;">
			//       <mark class="hl">Note:</mark> benefits are associated with <em>moderation</em>; risks increase with excessive intake.
			//     </p>
			//   </section>

			//   <section class="card antiox">
			//     <h3>Antioxidants and Homeostasis</h3>
			//     <ul>
			//       <li><strong>Protecting Cells:</strong> Antioxidants reduce oxidative stress, protecting cells and supporting overall health.</li>
			//       <li><strong>Supporting Immune Function:</strong> They enhance the immune response, helping maintain bodily balance.</li>
			//       <li><strong>Regulating Inflammation:</strong> Antioxidants help control inflammatory responses, essential for homeostasis.</li>
			//     </ul>
			//   </section>

			//   <section class="card video">
			//     <h3>Watch the Video</h3>
			//     <div class="video-embed">
			//       <iframe src="https://www.youtube.com/embed/3zk2fsfCyWU"
			//               title="SCI 101 Nutrition Crash Course"
			//               allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share"
			//               allowfullscreen></iframe>
			//     </div>
			//   </section>
			// </body>
			// </html>

			// 			`,
		},
		{
			name: "HTML Content",
			input: strings.TrimSpace(`
### https://openrouter.ai/

Features
Model Routing

Copy page

Dynamically route requests to models
OpenRouter provides two options for model routing.

Auto Router
The Auto Router, a special model ID that you can use to choose between selected high-quality models based on your prompt, powered by NotDiamond (https://www.notdiamond.ai/)[https://www.notdiamond.ai/]



			`),
		},
		// {
		// 	name:  "Empty String",
		// 	input: "",
		// },
	}

	for _, tc := range testCases {
		fmt.Printf("=== %s ===\n", tc.name)
		result := generator.GenerateHTML(tc.input)
		fmt.Printf("Input: %s\n", tc.input)
		fmt.Printf("Output length: %d characters\n", len(result))
		fmt.Printf("Output preview: %s...\n\n", result)
		// fmt.Printf("Output preview: %s...\n\n", result[:min(10, len(result))])

	}
}

func renderToHTML(content string) string {
	generator := NewHTMLGenerator()
	return generator.GenerateHTML(content)
}

// TitleOptions holds configuration for title extraction
type TitleOptions struct {
	MaxLength       int
	Fallback        string
	RemoveMarkdown  bool
	CapitalizeFirst bool
}

// DefaultTitleOptions returns the default options for title extraction
func DefaultTitleOptions() TitleOptions {
	return TitleOptions{
		MaxLength:       60,
		Fallback:        "Untitled",
		RemoveMarkdown:  true,
		CapitalizeFirst: true,
	}
}

// ExtractCleanTitle extracts and cleans a title for use in HTML head
func ExtractCleanTitle(input string, options ...TitleOptions) string {
	// Use default options if none provided
	opts := DefaultTitleOptions()
	if len(options) > 0 {
		opts = options[0]
	}

	if strings.TrimSpace(input) == "" {
		return opts.Fallback
	}

	title := strings.TrimSpace(input)

	// Extract title from various formats
	title = extractTitleFromFormats(title)

	// Remove markdown syntax if requested
	if opts.RemoveMarkdown {
		title = removeMarkdownSyntax(title)
	}

	// Remove HTML tags
	title = removeHTMLTags(title)

	// Decode HTML entities
	title = html.UnescapeString(title)

	// Clean up whitespace
	title = cleanWhitespace(title)

	// Capitalize first letter if requested
	if opts.CapitalizeFirst && len(title) > 0 {
		title = capitalizeFirst(title)
	}

	// Truncate if too long
	if len(title) > opts.MaxLength {
		if opts.MaxLength > 3 {
			title = title[:opts.MaxLength-3] + "..."
		} else {
			title = title[:opts.MaxLength]
		}
	}

	// Return fallback if title is empty after cleaning
	if strings.TrimSpace(title) == "" {
		return opts.Fallback
	}

	return title
}

// extractTitleFromFormats tries to extract title from various formats
func extractTitleFromFormats(input string) string {
	// 1. Look for markdown h1-h6 headers (# Title, ## Title, etc.)
	markdownHeaderRe := regexp.MustCompile(`^#+\s+(.+)$`)
	lines := strings.Split(input, "\n")
	for _, line := range lines {
		if match := markdownHeaderRe.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			return match[1]
		}
	}

	// 2. Look for HTML h1-h6 tags
	htmlHeaderRe := regexp.MustCompile(`<h[1-6][^>]*>(.*?)</h[1-6]>`)
	if match := htmlHeaderRe.FindStringSubmatch(input); match != nil {
		return match[1]
	}

	// 3. Use first non-empty line if no headers found
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}

	return input
}

// removeMarkdownSyntax removes common markdown syntax
func removeMarkdownSyntax(input string) string {
	// Remove bold/italic markers
	boldItalicRe := regexp.MustCompile(`\*{1,2}(.*?)\*{1,2}`)
	input = boldItalicRe.ReplaceAllString(input, "$1")

	underscoreRe := regexp.MustCompile(`_{1,2}(.*?)_{1,2}`)
	input = underscoreRe.ReplaceAllString(input, "$1")

	// Remove inline code
	inlineCodeRe := regexp.MustCompile("`([^`]+)`")
	input = inlineCodeRe.ReplaceAllString(input, "$1")

	// Remove strikethrough
	strikethroughRe := regexp.MustCompile(`~~(.*?)~~`)
	input = strikethroughRe.ReplaceAllString(input, "$1")

	// Remove links but keep text
	linkRe := regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	input = linkRe.ReplaceAllString(input, "$1")

	// Remove remaining markdown characters
	markdownCharsRe := regexp.MustCompile(`[#*_` + "`" + `~\[\]()]`)
	input = markdownCharsRe.ReplaceAllString(input, "")

	return input
}

// removeHTMLTags removes HTML tags from the input
func removeHTMLTags(input string) string {
	htmlTagRe := regexp.MustCompile(`<[^>]*>`)
	return htmlTagRe.ReplaceAllString(input, "")
}

// cleanWhitespace normalizes whitespace in the input
func cleanWhitespace(input string) string {
	// Replace multiple whitespace characters with single space
	whitespaceRe := regexp.MustCompile(`\s+`)
	input = whitespaceRe.ReplaceAllString(input, " ")
	return strings.TrimSpace(input)
}

// capitalizeFirst capitalizes the first letter of the input
func capitalizeFirst(input string) string {
	if len(input) == 0 {
		return input
	}

	runes := []rune(input)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

var mardownRenderer = map[string]string{
	".js":           "javascript",
	".jsx":          "javascript",
	".ts":           "typescript",
	".tsx":          "typescript",
	".py":           "python",
	".java":         "java",
	".c":            "c",
	".h":            "c",
	".cpp":          "cpp",
	".hpp":          "cpp",
	".cxx":          "cpp",
	".cc":           "cpp",
	".cs":           "csharp",
	".go":           "go",
	".rs":           "rust",
	".php":          "php",
	".swift":        "swift",
	".kt":           "kotlin",
	".kts":          "kotlin",
	".m":            "objectivec",
	".scala":        "scala",
	".sh":           "bash",
	".bash":         "bash",
	".zsh":          "bash",
	".bat":          "batch",
	".ps1":          "powershell",
	".html":         "html",
	".htm":          "html",
	".css":          "css",
	".scss":         "scss",
	".less":         "less",
	".json":         "json",
	".yaml":         "yaml",
	".yml":          "yaml",
	".xml":          "xml",
	".sql":          "sql",
	".r":            "r",
	".dart":         "dart",
	".lua":          "lua",
	".pl":           "perl",
	".pm":           "perl",
	".erl":          "erlang",
	".ex":           "elixir",
	".exs":          "elixir",
	".hs":           "haskell",
	".jl":           "julia",
	".lisp":         "lisp",
	".clj":          "clojure",
	".groovy":       "groovy",
	".md":           "markdown",
	".log":          "markdown",
	".csv":          "csv",
	".tsv":          "tsv",
	".txt":          "text",
	".dockerfile":   "dockerfile",
	".make":         "makefile",
	".mk":           "makefile",
	".toml":         "toml",
	".ini":          "ini",
	".cfg":          "ini",
	".conf":         "ini",
	".tex":          "latex",
	".bib":          "bibtex",
	".graphql":      "graphql",
	".gql":          "graphql",
	".yarn.lock":    "yaml",
	".package.json": "json",
}

func toHTML(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	targetSpecifier, found := mardownRenderer[ext]
	filename := filepath.Base(path)

	if !found {
		if imageExts[ext] || videoExts[ext] || audioExts[ext] || documentExts[ext] {
			return "", fmt.Errorf("no mardown specifier for: %s", ext)
		}

		if ext == "" {
			isUTF8, err := isUTF8FileSample(path, 4096)
			if err != nil || !isUTF8 {
				return "", fmt.Errorf("no mardown specifier for: %s", ext)
			}
		}

		targetSpecifier = mardownRenderer[".md"]
		// switch strings.ToLower(filename) {
		// case "todo", "help", "readme":
		// 	targetSpecifier = mardownRenderer[".md"]
		// case "Dockerfile":
		// 	targetSpecifier = mardownRenderer[".dockerfile"]

		// default:
		// 	return "", fmt.Errorf("no mardown specifier for: %s", ext)
		// }

	}

	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	generator := NewHTMLGenerator()
	generator.Title = filename
	generator.DontEnhanceURLs = true
	generator.IsFile = true
	metadata := fmt.Sprintf("| Property | Value |\n|---|---|\n| Size | %s |\n| Modified | %s |",
		format.HumanizeBytes(info.Size()),
		info.ModTime().Format("2006/01/02 15:04:05"),
	)
	header := "" // fmt.Sprintf("[Home](/)\n\n[Back](%s)\n\n## Index of %s\n\n", filepath.Dir(relPath), relPath)

	var result string
	if targetSpecifier == "markdown" {
		result = generator.GenerateHTML(fmt.Sprintf("%s\n\n# %s\n%s\n---\n\n%s\n", header, filename, metadata, string(bytes)))
	} else {
		result = generator.GenerateHTML(fmt.Sprintf("%s\n\n# %s\n%s\n---\n```%s\n%s\n```", header, filename, metadata, targetSpecifier, string(bytes)))
	}

	return result, nil
}

func isUTF8FileSample(filename string, sampleSize int64) (bool, error) {
	file, err := os.Open(filename)
	if err != nil {
		return false, err
	}
	defer file.Close()

	// Get file info to handle small files
	stat, err := file.Stat()
	if err != nil {
		return false, err
	}

	// Don't read more than the file size
	if sampleSize > stat.Size() {
		sampleSize = stat.Size()
	}

	if sampleSize == 0 {
		return true, nil // Empty file is valid UTF-8
	}

	// Read sample
	sample := make([]byte, sampleSize)
	_, err = file.Read(sample)
	if err != nil {
		return false, err
	}

	return utf8.Valid(sample), nil
}

// renderDirectoryIndex returns a Markdown-formatted directory listing.
func renderDirectoryIndex(basePath string, entries []os.DirEntry, relPath string, ignorePatterns []string) string {
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
