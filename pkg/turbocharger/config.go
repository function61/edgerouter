package turbocharger

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	configEnvName = "TURBOCHARGER_STORE"
)

func MiddlewareConfigAvailable() bool {
	return os.Getenv(configEnvName) != ""
}

func StorageFromConfig(ctx context.Context) (*CASPair, error) {
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

		regionID := urlParts.Host

		awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(regionID))
		if err != nil {
			return nil, err
		}

		s3Client := s3.NewFromConfig(awsConfig)

		return &CASPair{
			Files:     newS3Storage("turbocharger/files/", s3Client, bucketName),
			Manifests: newS3Storage("turbocharger/manifests/", s3Client, bucketName),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported scheme: %s", urlParts.Scheme)
	}
}
