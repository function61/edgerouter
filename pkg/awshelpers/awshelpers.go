package awshelpers

// TODO: move this to s3facade

import (
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/function61/gokit/aws/s3facade"
	"github.com/function61/gokit/envvar"
)

func Bucket(bucket string, region string) (*BucketContext, error) {
	creds, err := GetCredentials()
	if err != nil {
		return nil, err
	}

	s3Client, err := s3facade.Client(
		creds.AccessKeyId,
		creds.AccessKeySecret,
		region)
	if err != nil {
		return nil, err
	}

	return &BucketContext{
		Name: &bucket,
		S3:   s3Client,
	}, nil
}

type BucketContext struct {
	Name *string
	S3   *s3.S3
}

type Credentials struct {
	AccessKeyId     string
	AccessKeySecret string
}

func GetCredentials() (*Credentials, error) {
	accessKeyId, err := envvar.Required("AWS_ACCESS_KEY_ID")
	if err != nil {
		return nil, err
	}

	accessKeySecret, err := envvar.Required("AWS_SECRET_ACCESS_KEY")
	if err != nil {
		return nil, err
	}

	return &Credentials{accessKeyId, accessKeySecret}, nil
}
