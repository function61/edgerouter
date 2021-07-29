package turbocharger

import (
	"log"
	"net/http"
	"sync"
)

var (
	manifestHandlerSingleton = &struct {
		handler        *ManifestHandler
		handlerInitErr error
		mu             sync.Mutex
		once           sync.Once
	}{}
)

// you'll likely want to use a global instance since manifests are designed to be reloaded rapidly
// so there is no point in coupling the lifetime to a single manifest.
func GetManifestHandlerSingleton() (*ManifestHandler, error) {
	m := manifestHandlerSingleton // shorthand

	m.mu.Lock() // *once* is once, but does not protect from races to this getter
	defer m.mu.Unlock()

	m.once.Do(func() {
		m.handler, m.handlerInitErr = NewManifestHandlerAndStorages()
	})

	return m.handler, m.handlerInitErr
}

// doesn't error if middleware configuration not available.
// errors if middleware configuration is available but has error, or if errors initializing.
func WrapWithMiddlewareIfConfigAvailable(inner http.Handler, logger *log.Logger) (http.Handler, error) {
	if MiddlewareConfigAvailable() {
		manifestHandler, err := GetManifestHandlerSingleton()
		if err != nil {
			return nil, err
		}

		return NewMiddleware(inner, manifestHandler, logger), nil
	} else {
		return inner, nil
	}
}
