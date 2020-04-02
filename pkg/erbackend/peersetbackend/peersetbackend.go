// Reverse proxies traffic to a set of peers
package peersetbackend

import (
	"crypto/tls"
	"errors"
	"github.com/function61/edgerouter/pkg/erconfig"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

func New(opts erconfig.BackendOptsPeerSet) (http.Handler, error) {
	peerAddrs := []*url.URL{}

	for _, addr := range opts.Addrs {
		targetUrl, err := url.Parse(addr)
		if err != nil {
			return nil, err
		}

		peerAddrs = append(peerAddrs, targetUrl)
	}

	if len(peerAddrs) == 0 {
		return nil, errors.New("peersetbackend: empty peer list")
	}

	var transport http.RoundTripper
	if opts.TlsConfig != nil {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				ServerName:         opts.TlsConfig.ServerName,
				InsecureSkipVerify: opts.TlsConfig.InsecureSkipVerify,
			},
		}
	}

	rand.Seed(time.Now().Unix())

	// by default, the outgoing request includes the original "Host: ..." header, because
	// usually this is what we want (the backend can see what hostname is in browser's address bar)
	return &backend{&httputil.ReverseProxy{
		Transport: transport,
		Director: func(req *http.Request) {
			randomPeerIdx := rand.Intn(len(peerAddrs))

			peerUrl := peerAddrs[randomPeerIdx]

			req.URL.Scheme = peerUrl.Scheme
			req.URL.Host = peerUrl.Host // can include port
		},
	}}, nil
}

type backend struct {
	reverseProxy *httputil.ReverseProxy
}

func (b *backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.reverseProxy.ServeHTTP(w, r)
}
