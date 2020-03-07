package erserver

import (
	"html/template"
	"log"
	"net/http"
)

type adminBackendImpl struct {
	fem *frontendMatchers
	tpl *template.Template
}

func newAdminBackend(fem *frontendMatchers) http.Handler {
	tpl, err := template.New("_").Parse(`
<html>
<head>
	<title>edgerouter admin</title>
</head>

<body>

<pre>
{{range .}}
{{.Describe}}

{{end}}
</pre>

</body>
</html>
`)
	if err != nil {
		panic(err)
	}

	return &adminBackendImpl{
		fem,
		tpl,
	}
}

func (a *adminBackendImpl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := a.tpl.Execute(w, a.fem.Apps); err != nil {
		log.Printf("adminui: template error: %v", err)
	}
}
