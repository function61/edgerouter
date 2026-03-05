package turbocharger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type s3Storage struct {
	prefix     string
	bucketName string
	s3Client   *s3.Client
}

var _ CAS = (*s3Storage)(nil)

func newS3Storage(prefix string, s3Client *s3.Client, bucketName string) CAS {
	return &s3Storage{
		prefix:     prefix,
		bucketName: bucketName,
		s3Client:   s3Client,
	}
}

var _ CAS = (*s3Storage)(nil)

func (d *s3Storage) GetObject(ctx context.Context, id ObjectID) (io.ReadCloser, error) {
	response, err := d.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(d.bucketName),
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

	_, err = d.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(d.bucketName),
		Key:         aws.String(d.path(id)),
		Body:        bytes.NewReader(buffered),
		ContentType: aws.String(contentType),
	})
	return err
}

func (d *s3Storage) exists(ctx context.Context, id ObjectID) (bool, error) {
	_, err := d.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(d.bucketName),
		Key:    aws.String(d.path(id)),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NotFound" { // documentation incorrectly says NoSuchKey
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
