package erserver

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/function61/gokit/httputils"
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

func newAdminBackend(fem *frontendMatchers) (http.Handler, error) {
	pageRendered, err := renderPage(fem)
	if err != nil {
		return nil, err
	}

	pages := http.NewServeMux()
	pages.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// root handler is special that does catch-all, so we've to filter for it if
		// we don't wish for everything to exist
		if r.URL.Path != "/" {
			httputils.Error(w, http.StatusNotFound)
			return
		}

		fmt.Fprintln(w, pageRendered)
	})

	return pages, nil
}

func renderPage(fem *frontendMatchers) (string, error) {
	tpl, err := template.New("_").Parse(adminTpl)
	if err != nil {
		return "", err
	}

	appDescriptions := []string{}
	for _, app := range fem.Apps {
		appDescriptions = append(appDescriptions, app.Describe())
	}

	pageRendered := &strings.Builder{}

	if err := tpl.Execute(pageRendered, appDescriptions); err != nil {
		return "", fmt.Errorf("adminui: template error: %v", err)
	}

	return pageRendered.String(), nil
}
