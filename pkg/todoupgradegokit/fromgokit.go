package todoupgradegokit

import "time"

var (
	// this has to be given always when making a server to mitigate slowrosis attack:
	//   https://en.wikipedia.org/wiki/Slowloris_(computer_security)
	// value same as nginx: https://www.oreilly.com/library/view/nginx-http-server/9781788623551/0b1ce6c8-4863-433c-bb70-bf9aa565654c.xhtml
	DefaultReadHeaderTimeout = 60 * time.Second
)
