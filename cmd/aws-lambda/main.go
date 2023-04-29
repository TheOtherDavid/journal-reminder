package main

import (
	"context"
	"github.com/TheOtherDavid/journal-reminder"
	"github.com/aws/aws-lambda-go/lambda"
	"math/rand"
	"time"
)

func main() {
	lambda.Start(handleRequest)
}

type MyEvent struct {
	DocumentIds []string `json:"documentIds"`
}

func handleRequest(ctx context.Context, event MyEvent) (string, error) {
	documentIds := event.DocumentIds
	//Choose a random year to send the reminder for
	rand.Seed(time.Now().UnixNano())
	randIndex := rand.Intn(len(documentIds))
	documentId := documentIds[randIndex]
	remind.Remind([]string{documentId})
	return "Success", nil
}
