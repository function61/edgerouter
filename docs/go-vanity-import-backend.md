# Go vanity import backend

## Declaring a module path

For a repository hosted at `https://github.com/myorg/myapp`, declare the stable public import
path in that repository's `go.mod`:

```go
module example.com/myapp
```

Add a matching `/myapp` frontend and use `https://github.com/myorg` as the repository URL
prefix. Go then resolves `example.com/myapp` through Edgerouter and clones the GitHub
repository. If the repository later moves to another forge, update only
`repository_url_prefix`; consumers keep importing `example.com/myapp`.

The `go_import` backend serves stable Go module paths while redirecting browsers to the
current source-control forge. Configure one application for a source-control namespace and
one hostname frontend for each exported module. This reserves only those paths, allowing the
same hostname to continue serving a website.

```json
{
  "id": "go-imports-myorg",
  "frontends": [
    {
      "kind": "hostname",
      "hostname": "example.com",
      "path_prefix": "/mylib1"
    },
    {
      "kind": "hostname",
      "hostname": "example.com",
      "path_prefix": "/mylib2"
    }
  ],
  "backend": {
    "kind": "go_import",
    "go_import_opts": {
      "repository_url_prefix": "https://github.com/myorg"
    }
  }
}
```

`go get example.com/mylib1` retrieves `https://github.com/myorg/mylib1`. Browser requests
use a temporary redirect, so the forge address can be changed later without a cached permanent
redirect. Configure explicit module path prefixes to reserve only those website paths. The
backend uses the first request path component as the project name.
