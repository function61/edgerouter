package erserver

import (
	"context"
	"log"
	"time"

	"github.com/function61/edgerouter/pkg/erdiscovery"
	"github.com/function61/gokit/logex"
)

func scheduledSync(
	ctx context.Context,
	discovery erdiscovery.Reader,
	configUpdated chan<- *frontendMatchers,
	logger *log.Logger,
) error {
	logl := logex.Levels(logger)

	// if we used S3 discovery backend:
	//   - pricing: "$0.005 per LIST 1,000 reqs"
	//   - every 5 secs => 17 280 reqs/day => 525 600 reqs/month = 2.628 $/month per loadbalancer
	// update: EventHorizon discovery is now the preferred (and cheaper method)
	ticker := time.NewTicker(10 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			conf, err := syncAppsFromDiscovery(ctx, discovery, logl)
			if err != nil {
				logl.Error.Printf("syncAppsFromDiscovery: %v", err)
				continue
			}

			select {
			case configUpdated <- conf:
			default:
				// if we tried to block, we could block forever if consumer went away
				// (already exited for example)
				logl.Error.Println("configUpdated blocks")
			}
		}
	}

}
