package main

import (
	"bytes"
	"encoding/csv"
	"fmt"

	"github.com/TheOtherDavid/journal-reminder"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

	"net/http"
	"os"
	"strconv"
	"strings"
)

func main() {
	lambda.Start(handleRequest)
}

func handleRequest() (string, error) {
	remind.Remind()
	return "Success", nil
}
