package statics3websitebackend

import (
	"time"
)

// this file is uploaded to the bucket mainly to support enumerating old versions for
// cleanup purposes. cleanup could be implemented by checking for currently deployed
// version, and enumerating deployments older than that, and then removing them
type deploymentSpec struct {
	Version    string    `json:"version"`
	DeployedAt time.Time `json:"deployed_at"`
}
