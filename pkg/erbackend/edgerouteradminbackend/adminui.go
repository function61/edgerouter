// Admin panel for Edgerouter
package edgerouteradminbackend

import (
	"html/template"
	"io"
	"net/http"

	"github.com/function61/edgerouter/pkg/erconfig"
)

const adminTpl = `
<html>
<head>
	<title>edgerouter admin</title>
</head>

<body>

<pre>
{{range .}}
{{.}}

{{end}}
</pre>

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

func renderPage(apps erconfig.CurrentConfigAccessor, output io.Writer) error {
	tpl, err := template.New("_").Parse(adminTpl)
	if err != nil {
		return err
	}

	appDescriptions := []string{}
	for _, app := range apps() {
		appDescriptions = append(appDescriptions, app.Describe())
	}

	return tpl.Execute(output, appDescriptions)
}
