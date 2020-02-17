// Reverse proxies traffic to a set of peers
package peersetbackend

import (
	"crypto/tls"
	"github.com/function61/edgerouter/pkg/erbackend"
	"github.com/function61/edgerouter/pkg/erconfig"
	"net/http"
	"net/http/httputil"
	"net/url"
)

func New(opts erconfig.BackendOptsPeerSet) erbackend.Backend {
	// FIXME
	firstAddr, err := url.Parse(opts.Addrs[0])
	if err != nil {
		panic(err)
	}

	rp := httputil.NewSingleHostReverseProxy(firstAddr)

	if opts.TlsConfig != nil {
		rp.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				ServerName:         opts.TlsConfig.ServerName,
				InsecureSkipVerify: opts.TlsConfig.InsecureSkipVerify,
			},
		}
	}

	return &backend{
		reverseProxy: rp,
	}
}

type backend struct {
	reverseProxy *httputil.ReverseProxy
}

func (b *backend) Serve(w http.ResponseWriter, r *http.Request) {
	b.reverseProxy.ServeHTTP(w, r)
}
