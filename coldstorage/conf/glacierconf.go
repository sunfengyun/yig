package conf

import (
	"github.com/aws/aws-sdk-go/aws"
	. "github.com/aws/aws-sdk-go/aws/credentials"
)

func ToSessConfig(endpoint, region, ak, sk string) *aws.Config {
	SessConfig := &aws.Config{
		Credentials:      NewStaticCredentials(ak, sk, ""),
		Endpoint:         aws.String(endpoint),
		Region:           aws.String(region),
		DisableSSL:       aws.Bool(true),
		S3ForcePathStyle: aws.Bool(true),
	}
	return SessConfig
}
