package main

import (
	"github.com/TheOtherDavid/journal-reminder"
	"os"
)

func main() {
	documentIds := os.Args[1:]
	remind.Remind(documentIds)
}
