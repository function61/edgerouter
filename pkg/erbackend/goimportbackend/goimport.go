// Serves Go import metadata and repository redirects.
//
// Go VCS discovery: https://go.dev/ref/mod#vcs-find
// See https://iain.rocks/blog/dont-couple-your-go-code-to-github
package goimportbackend

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/function61/edgerouter/pkg/erconfig"
)

type backend struct {
	repositoryURLPrefix *url.URL
}

func New(opts *erconfig.BackendOptsGoImport) (*backend, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	repositoryURLPrefix, err := url.Parse(opts.RepositoryURLPrefix)
	if err != nil {
		return nil, err
	}

	return &backend{repositoryURLPrefix: repositoryURLPrefix}, nil
}

func (b *backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	project, projectPath, found := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if project == "" {
		http.Error(w, "go_import request path must name a project", http.StatusBadRequest)
		return
	}

	repositoryURL := *b.repositoryURLPrefix
	repositoryURL.Path = path.Join(repositoryURL.Path, project)
	repositoryURL.RawPath = ""

	if isGoTool := r.URL.Query().Get("go-get") == "1"; isGoTool {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(
			w,
			"<!doctype html><html><head><meta name=\"go-import\" content=\"%s git %s\"></head></html>",
			html.EscapeString(r.Host+"/"+project),
			html.EscapeString(repositoryURL.String()))
		return
	}

	target := repositoryURL
	if found {
		target.Path = path.Join(target.Path, projectPath)
	}
	target.RawPath = ""
	target.RawQuery = r.URL.RawQuery
	target.ForceQuery = r.URL.ForceQuery
	http.Redirect(w, r, target.String(), http.StatusFound)
}
