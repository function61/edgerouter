// Admin panel for Edgerouter
package edgerouteradminbackend

import (
	"html/template"
	"io"
	"net/http"
	"time"

	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/gokit/dynversion"
)

const adminTpl = `
<html>
<head>
	<title>edgerouter admin</title>
</head>

<body>

<pre>
{{range .Apps}}
{{.}}

{{end}}
</pre>

<p>Version: {{.Version}}</p>
<p>LastUpdated: {{.LastUpdated}}</p>
</body>
</html>
`

func New(currentConfig erconfig.CurrentConfigAccessor) (http.Handler, error) {
	pages := http.NewServeMux()
	pages.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// root handler is special that does catch-all, so we've to filter for it if
		// we don't wish for everything to exist
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		_ = renderPage(currentConfig, w)
	})

	return pages, nil
}

func renderPage(currentConfig erconfig.CurrentConfigAccessor, output io.Writer) error {
	tpl, err := template.New("_").Parse(adminTpl)
	if err != nil {
		return err
	}

	appDescriptions := []string{}
	for _, app := range currentConfig.Apps() {
		appDescriptions = append(appDescriptions, app.Describe())
	}

	return tpl.Execute(output, struct {
		Apps        []string
		Version     string
		LastUpdated string
	}{
		Apps:        appDescriptions,
		Version:     dynversion.Version,
		LastUpdated: currentConfig.LastUpdated().Format(time.RFC3339),
	})
}
