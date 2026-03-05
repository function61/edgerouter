// Discover application from S3 bucket (EventHorizon-based discovery is highly recommended instead)
package s3discovery

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // Not used for cryptographic purposes
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery"
	"github.com/function61/gokit/envvar"
)

func HasConfigInEnv() bool {
	// intentionally allows one to be empty (validation is done in New() anyway)
	return os.Getenv("S3_DISCOVERY_BUCKET") != "" || os.Getenv("S3_DISCOVERY_BUCKET_REGION") != ""
}

type s3discovery struct {
	bucketName     string
	s3Client       *s3.Client
	cachedRead     []erconfig.Application
	cachedReadHash []byte
}

func New(ctx context.Context) (erdiscovery.ReaderWriter, error) {
	bucketName, err := envvar.Required("S3_DISCOVERY_BUCKET")
	if err != nil {
		return nil, err
	}

	region, err := envvar.Required("S3_DISCOVERY_BUCKET_REGION")
	if err != nil {
		return nil, err
	}

	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return nil, err
	}

	return &s3discovery{
		bucketName: bucketName,
		s3Client:   s3.NewFromConfig(awsConfig),
		cachedRead: []erconfig.Application{},
	}, nil
}

func (d *s3discovery) ReadApplications(ctx context.Context) ([]erconfig.Application, error) {
	listResponse, err := d.s3Client.ListObjects(ctx, &s3.ListObjectsInput{
		Bucket: aws.String(d.bucketName),
		Prefix: aws.String("discovery/"),
	})
	if err != nil {
		return nil, fmt.Errorf("s3discovery: ListObjects: %v", err)
	}

	//nolint:gosec // Not used for cryptographic purposes
	contentsEtagsHashBuilder := sha1.New()

	for _, object := range listResponse.Contents {
		if _, err := contentsEtagsHashBuilder.Write([]byte(*object.ETag + "\n")); err != nil {
			return nil, err
		}
	}

	contentsEtagsHash := contentsEtagsHashBuilder.Sum(nil)

	if bytes.Equal(d.cachedReadHash, contentsEtagsHash) {
		return d.cachedRead, nil
	}

	apps := []erconfig.Application{}

	for _, object := range listResponse.Contents {
		objectResponse, err := d.s3Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(d.bucketName),
			Key:    object.Key,
		})
		if err != nil {
			return nil, err
		}

		app := erconfig.Application{}

		decoder := json.NewDecoder(objectResponse.Body)
		decoder.DisallowUnknownFields()
		if err := func() error {
			defer objectResponse.Body.Close()

			return decoder.Decode(&app)
		}(); err != nil {
			return nil, err
		}

		apps = append(apps, app)
	}

	d.cachedRead = apps
	d.cachedReadHash = contentsEtagsHash

	return apps, nil
}

func (d *s3discovery) UpdateApplication(ctx context.Context, app erconfig.Application) error {
	buf, err := json.MarshalIndent(&app, "", "  ")
	if err != nil {
		return err
	}

	if _, err := d.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(d.bucketName),
		Key:         aws.String(discoveryFilePath(app.ID)),
		ContentType: aws.String("application/json"),
		Body:        bytes.NewReader(buf),
	}); err != nil {
		return fmt.Errorf("s3discovery: PutObject: %v", err)
	}

	return nil
}

func (d *s3discovery) DeleteApplication(ctx context.Context, app erconfig.Application) error {
	if _, err := d.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(d.bucketName),
		Key:    aws.String(discoveryFilePath(app.ID)),
	}); err != nil {
		return fmt.Errorf("s3discovery: DeleteObject: %v", err)
	}

	return nil
}

func discoveryFilePath(appID string) string {
	return fmt.Sprintf("discovery/%s.json", appID)
}
