package turbocharger

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/function61/gokit/aws/s3facade"
)

const (
	configEnvName = "TURBOCHARGER_STORE"
)

func MiddlewareConfigAvailable() bool {
	return os.Getenv(configEnvName) != ""
}

func StorageFromConfig() (*CASPair, error) {
	conf := os.Getenv(configEnvName)
	if conf == "" {
		return nil, fmt.Errorf("ENV not specified: %s", configEnvName)
	}

	urlParts, err := url.Parse(conf)
	if err != nil {
		return nil, err
	}

	switch urlParts.Scheme {
	case "s3":
		// s3://region/bucket

		// "/bucket" => "bucket"
		bucketName := strings.TrimLeft(urlParts.Path, "/")

		regionId := urlParts.Host

		bucket, err := s3facade.Bucket(bucketName, nil, regionId)
		if err != nil {
			return nil, err
		}

		return &CASPair{
			Files:     newS3Storage("turbocharger/files/", bucket),
			Manifests: newS3Storage("turbocharger/manifests/", bucket),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported scheme: %s", urlParts.Scheme)
	}
}
