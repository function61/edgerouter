package turbocharger

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/function61/gokit/aws/s3facade"
)

type s3Storage struct {
	prefix string
	bucket *s3facade.BucketContext
}

var _ CAS = (*s3Storage)(nil)

func newS3Storage(prefix string, bucket *s3facade.BucketContext) CAS {
	return &s3Storage{prefix, bucket}
}

var _ CAS = (*s3Storage)(nil)

func (d *s3Storage) GetObject(ctx context.Context, id ObjectID) (io.ReadCloser, error) {
	response, err := d.bucket.S3.GetObjectWithContext(ctx, &s3.GetObjectInput{
		Bucket: d.bucket.Name,
		Key:    aws.String(d.path(id)),
	})
	if err != nil {
		return nil, err
	}

	return response.Body, nil
}

func (d *s3Storage) InsertObject(ctx context.Context, id ObjectID, content io.Reader, contentType string) error {
	exists, err := d.exists(ctx, id)
	if err != nil {
		return err
	}

	if exists {
		return nil
	}

	// need to buffer because s3 client needs io.ReadSeeker
	buffered, err := io.ReadAll(content)
	if err != nil {
		return err
	}

	_, err = d.bucket.S3.PutObjectWithContext(ctx, &s3.PutObjectInput{
		Bucket:      d.bucket.Name,
		Key:         aws.String(d.path(id)),
		Body:        bytes.NewReader(buffered),
		ContentType: aws.String(contentType),
	})
	return err
}

func (d *s3Storage) exists(ctx context.Context, id ObjectID) (bool, error) {
	_, err := d.bucket.S3.HeadObjectWithContext(ctx, &s3.HeadObjectInput{
		Bucket: d.bucket.Name,
		Key:    aws.String(d.path(id)),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok && aerr.Code() == "NotFound" { // documentation incorrectly says s3.ErrCodeNoSuchKey
			return false, nil
		} else { // some other error
			return false, fmt.Errorf("exists: %w", err)
		}
	} else { // no error => object exists
		return true, nil
	}
}

func (d *s3Storage) path(id ObjectID) string {
	return d.prefix + id.String()
}
