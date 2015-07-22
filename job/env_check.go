package job

import (
	"config"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
)

func init() {
	if config.Get("AWS_ACCESS_KEY_ID") == "" {
		panic("AWS_ACCESS_KEY_ID environmental variable not specified.")
	}

	if config.Get("AWS_SECRET_ACCESS_KEY") == "" {
		panic("AWS_SECRET_ACCESS_KEY environmental variable not specified.")
	}

	aws.DefaultConfig.Credentials = credentials.NewStaticCredentials(
		config.Get("AWS_ACCESS_KEY_ID"),
		config.Get("AWS_SECRET_ACCESS_KEY"), "")

	if config.Get("AWS_REGION") == "" {
		panic("AWS_REGION environmental variable not specified.")
	}

	aws.DefaultConfig.Region = config.Get("AWS_REGION")

}
