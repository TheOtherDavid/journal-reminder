package main

import (
	"context"
	"github.com/TheOtherDavid/journal-reminder"
	"github.com/aws/aws-lambda-go/lambda"
)

func main() {
	lambda.Start(handleRequest)
}

type MyEvent struct {
	DocumentIDs []string `json:"documentIds"`
}

func handleRequest(ctx context.Context, event MyEvent) (string, error) {
	documentIds := event.DocumentIDs
	remind.Remind(documentIds)
	return "Success", nil
}
