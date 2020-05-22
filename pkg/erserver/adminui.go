package erserver

import (
	"html/template"
	"log"
	"net/http"
)

type adminBackendImpl struct {
	appDescriptions []string
	tpl             *template.Template
}

func newAdminBackend(fem *frontendMatchers) (http.Handler, error) {
	tpl, err := template.New("_").Parse(`
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
`)
	if err != nil {
		return nil, err
	}

	appDescriptions := []string{}
	for _, app := range fem.Apps {
		appDescriptions = append(appDescriptions, app.Describe())
	}

	return &adminBackendImpl{
		appDescriptions,
		tpl,
	}, nil
}

func (a *adminBackendImpl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := a.tpl.Execute(w, a.appDescriptions); err != nil {
		log.Printf("adminui: template error: %v", err)
	}
}
