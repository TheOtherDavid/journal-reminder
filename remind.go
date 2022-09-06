package remind

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func Remind(documentIds []string) error {
	//TODO: Join the array together with commas in a string to take multiple items
	documentString := documentIds[0]
	//TODO: Refresh Google Oauth.
	//For now, just use an env variable and refresh it manually so we can get to the fun coding faster.
	accessToken := os.Getenv("GCP_AUTH_CODE")

	//Find file in Google Docs by filename
	url := "https://docs.googleapis.com/v1/documents/" + documentString

	client := http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Oh no, error.")
		return err
	}

	req.Header = http.Header{
		"Authorization": {"Bearer " + accessToken},
	}
	response, err := client.Do(req)
	if err != nil {
		fmt.Println("Oh no, error.")
		return err
	}
	defer response.Body.Close()

	var responseObject GoogleDocumentResponse
	err = json.NewDecoder(response.Body).Decode(&responseObject)
	if err != nil {
		fmt.Println("Oh no, error.")
		return err
	}
	//Take the target year from the journal title
	targetYear := responseObject.Title[8:]
	targetYearInt, _ := strconv.Atoi(targetYear)
	currentTime := time.Now()
	targetDate := time.Date(targetYearInt, currentTime.Month(), currentTime.Day(), 0, 0, 0, 0, time.UTC)
	//currentDayString := currentTime.Format("1/2/2006")

	append := false
	message := ""

	for _, contentItem := range responseObject.Body.Content {
		//Parse file, specifically try to look for headers.
		//If the current content list's text is type Heading 4, then we need to append the NEXT several paragraphs to a message.
		//Then we need to do that UNTIL there's another Heading.
		if contentItem.Paragraph.ParagraphStyle.NamedStyleType == "HEADING_4" {
			if append == true {
				//If we encounter a header when in append mode, we turn off append mode
				append = false
				continue
			}
			if append == false {
				//If we aren't in append mode, check which date format to use
				textRunContent := contentItem.Paragraph.Elements[0].TextRun.Content
				if textRunContent != "" && textRunContent != "\n" {
					//check the date on the entry
					dateString := strings.Split(textRunContent, ",")[0]
					headingDate := time.Time{}
					layout := ""

					layout = "1/2/2006"
					//Convert to date
					headingDate, err := time.Parse(layout, dateString)
					if err != nil {
						fmt.Println("English date parse failed, trying European.")
						layout = "2/1/2006"
						headingDate, err = time.Parse(layout, dateString)
						if err != nil {
							fmt.Println("European date parse failed too. What's wrong?")
							return err
						}
					}
					//Now we need to compare the target (current) date to the heading date, to see if this is today's entry.
					if headingDate.Equal(targetDate) {
						//If the heading is the current day, enter append mode
						append = true
					}
				}
			}
		}
		//If in append mode, append message
		if append == true {
			for _, element := range contentItem.Paragraph.Elements {
				message = message + element.TextRun.Content
			}
		}
	}
	//Once we have the appropriate journal entries, compose them into an e-mail message
	fmt.Println(message)

	//TODO: Send the e-mail to my personal account, using my bot e-mail account.
	return nil
}

type GoogleDocumentResponse struct {
	Title      string             `json:"title"`
	Body       GoogleDocumentBody `json:"body"`
	DocumentId string             `json:"documentId"`
}

type GoogleDocumentBody struct {
	Content []GoogleDocumentContentItem `json:"content"`
}
type GoogleDocumentContentItem struct {
	StartIndex int                     `json:"startIndex"`
	EndIndex   int                     `json:"endIndex"`
	Paragraph  GoogleDocumentParagraph `json:"paragraph"`
}

type GoogleDocumentParagraph struct {
	Elements       []GoogleDocumentElement      `json:"elements"`
	ParagraphStyle GoogleDocumentParagraphStyle `json:"paragraphStyle"`
}

type GoogleDocumentElement struct {
	StartIndex int                   `json:"startIndex"`
	EndIndex   int                   `json:"endIndex"`
	TextRun    GoogleDocumentTextRun `json:"textRun"`
}

type GoogleDocumentParagraphStyle struct {
	HeadingId       string `json:"headingId"`
	NamedStyleType  string `json:"namedStyleType"`
	Direction       string `json:"direction"`
	PageBreakBefore bool   `json:"pageBreakBefore"`
}

type GoogleDocumentTextRun struct {
	Content string `json:"content"`
}
