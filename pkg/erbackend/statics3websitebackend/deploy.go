package statics3websitebackend

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/function61/edgerouter/pkg/erconfig"
	"github.com/function61/edgerouter/pkg/erdiscovery"
	"github.com/function61/gokit/mime"
)

type uploadJob struct {
	applicationID  string
	deploymentSpec deploymentSpec
	bucketName     string
	s3Client       *s3.Client
}

// atomically deploys a new version of a site to a S3 bucket, then updates service
// discovery to point to the new deployed version
func Deploy(ctx context.Context, tarArchive io.Reader, applicationID string, deployVersion string, discoverySvc erdiscovery.ReaderWriter) error {
	apps, err := discoverySvc.ReadApplications(ctx)
	if err != nil {
		return err
	}

	app := erconfig.FindApplication(applicationID, apps)
	if app == nil {
		return fmt.Errorf("unknown applicationId: %s", applicationID)
	}

	if app.Backend.Kind != erconfig.BackendKindS3StaticWebsite {
		return fmt.Errorf("expecting %s", erconfig.BackendKindS3StaticWebsite)
	}

	s3StaticWebsiteOpts := app.Backend.S3StaticWebsiteOpts

	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(s3StaticWebsiteOpts.RegionID))
	if err != nil {
		return err
	}

	upload := &uploadJob{
		applicationID: app.ID,
		deploymentSpec: deploymentSpec{
			Version:    deployVersion,
			DeployedAt: time.Now(),
		},
		bucketName: s3StaticWebsiteOpts.BucketName,
		s3Client:   s3.NewFromConfig(awsConfig),
	}

	if err := uploadAllFiles(ctx, tarArchive, upload); err != nil {
		return fmt.Errorf("uploadAllFiles: %w", err)
	}

	// update deployed version pointer in the discovery to reflect changes
	// to all edge routers
	app.Backend = erconfig.S3Backend(
		s3StaticWebsiteOpts.BucketName,
		s3StaticWebsiteOpts.RegionID,
		upload.deploymentSpec.Version)

	return discoverySvc.UpdateApplication(ctx, *app)
}

func uploadAllFiles(ctx context.Context, tarArchive io.Reader, upload *uploadJob) error {
	unzipped, err := gzip.NewReader(tarArchive)
	if err != nil {
		return err
	}

	// n=1 => 43 s
	// n=2 => 25 s
	// n=3 => 19 s
	workerCount := 2
	workItems := make(chan *s3.PutObjectInput, workerCount)
	workError := make(chan error, workerCount)

	wg := &sync.WaitGroup{}
	wg.Add(workerCount)

	for i := 0; i < workerCount; i++ {
		go uploadWorker(ctx, upload.s3Client, workItems, workError, wg)
	}

	// cancel and wait for workers exit in all exit paths
	var coaw sync.Once
	closeOnceAndWait := func() {
		coaw.Do(func() {
			close(workItems)
			wg.Wait()
		})
	}
	defer closeOnceAndWait()

	tarReader := tar.NewReader(unzipped)
	for {
		entry, err := tarReader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}

			return err
		}

		// skip over dirs
		if entry.FileInfo().IsDir() {
			continue
		}

		ur, err := createUploadRequest(entry.Name, tarReader, upload)
		if err != nil {
			return err
		}

		select {
		case workItems <- ur:
		case err := <-workError:
			return err
		}
	}

	// deployment spec
	deploymentSpecJSON, err := json.MarshalIndent(&upload.deploymentSpec, "", "  ")
	if err != nil {
		return err
	}

	workItems <- &s3.PutObjectInput{
		Bucket:      aws.String(upload.bucketName),
		Key:         aws.String(bucketPrefix(upload.applicationID, upload.deploymentSpec.Version) + ".deployment.json"),
		ContentType: aws.String("application/json"),
		Body:        bytes.NewReader(deploymentSpecJSON),
	}

	closeOnceAndWait()

	select {
	case err := <-workError:
		return err
	default:
		return nil
	}
}

// filePath looks like "images/2018/unificontroller-stats.png"
func createUploadRequest(filePath string, content io.Reader, upload *uploadJob) (*s3.PutObjectInput, error) {
	// stupid S3 client requires io.ReadSeeker, so we've to read the entire file in memory......
	wholeFileInMemory, err := io.ReadAll(content)
	if err != nil {
		return nil, err
	}

	ext := filepath.Ext(filePath)

	// looks like "joonasfi-blog/versionid/"
	pathPrefix := bucketPrefix(upload.applicationID, upload.deploymentSpec.Version) + "/"

	// sometimes the entries start with a dot, and we would end up with
	// "sites/APP_ID/VERSION/./readme.md" unless we normalize this
	fullPath := path.Clean(pathPrefix + filePath)

	return &s3.PutObjectInput{
		Bucket:       aws.String(upload.bucketName),
		Key:          aws.String(fullPath),
		ContentType:  aws.String(mime.TypeByExtension(ext, mime.OctetStream)),
		CacheControl: aws.String("max-age=31536000"), // 1 year (= "infinite caching"), because this versioned URL will never change
		Body:         bytes.NewReader(wholeFileInMemory),
	}, nil
}

func uploadWorker(ctx context.Context, s3Client *s3.Client, objects <-chan *s3.PutObjectInput, workError chan<- error, wg *sync.WaitGroup) {
	defer wg.Done()

	uploadOneObject := func(object *s3.PutObjectInput) error {
		ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		defer cancel()

		slog.Info("uploading", "key", *object.Key)

		_, err := s3Client.PutObject(ctx, object)
		return err
	}

	for object := range objects {
		if err := uploadOneObject(object); err != nil {
			workError <- err
			return
		}
	}
}

// looks like "sites/joonasfi-blog/versionid"
func bucketPrefix(applicationID string, deployVersion string) string {
	return "sites/" + applicationID + "/" + deployVersion
}
