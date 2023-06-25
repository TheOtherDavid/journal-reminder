package remind

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	docs "google.golang.org/api/docs/v1"
	"google.golang.org/api/option"
)

func Remind(documentIds []string) (err error) {
	// Create a new Docs service client with the token source
	client, err := getDocsService()
	if err != nil {
		log.Fatalf("Unable to create client: %v", err)
	}

	message := "\n"

	for _, documentID := range documentIds {
		// Call the API to retrieve the document content
		doc, err := client.Documents.Get(documentID).Do()
		if err != nil {
			log.Fatalf("Unable to retrieve document: %v", err)
		}

		// Print the document content
		fmt.Printf("The document title is: %s\n", doc.Title)
		//fmt.Printf("The document content is: %s\n", doc.Body.Content)

		//Maybe this could be pulled into a "extract journal entry" function

		//Take the target year from the journal title
		targetYear := doc.Title[8:]
		targetYearInt, _ := strconv.Atoi(targetYear)
		currentTime := time.Now()
		targetDate := time.Date(targetYearInt, currentTime.Month(), currentTime.Day(), 0, 0, 0, 0, time.UTC)
		//currentDayString := currentTime.Format("1/2/2006")

		append := false
		done := false

		for _, contentItem := range doc.Body.Content {
			//Parse file, specifically try to look for headers.
			//If the current content list's text is type Heading 4, then we need to append the NEXT several paragraphs to a message.
			//Then we need to do that UNTIL there's another Heading.
			if done {
				break
			}
			if contentItem.Paragraph != nil && contentItem.Paragraph.ParagraphStyle.NamedStyleType == "HEADING_4" {
				if append == true {
					//If we encounter a header when in append mode, we turn off append mode
					append = false
					done = true
					break
				}
				if append == false {
					//If we aren't in append mode, check which date format to use
					element := contentItem.Paragraph.Elements[0]
					if element != nil {
						if element.TextRun != nil {
							textRunContent := element.TextRun.Content
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
										fmt.Println(dateString)
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
				}
			}
			//If in append mode, append message
			if append == true {
				for _, element := range contentItem.Paragraph.Elements {
					if element.PageBreak == nil {
						message = message + element.TextRun.Content
					} else {
						fmt.Println("Page break detected.")
						append = false
						done = true
						break
					}
				}
			}
		}
	}
	//Once we have the appropriate journal entries, compose them into an e-mail message
	fmt.Println(message)

	//TODO: Send the e-mail to my personal account, using my bot e-mail account.
	senderEmail := os.Getenv("SENDER_EMAIL")
	senderPassword := os.Getenv("SENDER_PASSWORD")
	recipientEmail := os.Getenv("RECIPIENT_EMAIL")
	if senderEmail == "" {
		return errors.New("SENDER_EMAIL env variable not set")
	}
	if senderPassword == "" {
		return errors.New("SENDER_PASSWORD env variable not set")
	}
	if recipientEmail == "" {
		return errors.New("RECIPIENT_EMAIL env variable not set")
	}
	fmt.Println("Sending email")
	err = SendEmail(recipientEmail, senderEmail, senderPassword, message)
	fmt.Println("Email sent.")
	if err != nil {
		fmt.Println(err)
		return err
	}
	return nil
}

func getToken(config *oauth2.Config) (*oauth2.Token, error) {
	// Try to read the token from a file
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err == nil {
		return tok, nil
	}

	// If the token is not found or is invalid, obtain a new one
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var code string
	if _, err := fmt.Scan(&code); err != nil {
		return nil, fmt.Errorf("Unable to read authorization code: %v", err)
	}

	tok, err = config.Exchange(context.Background(), code)
	if err != nil {
		return nil, fmt.Errorf("Unable to retrieve token from web: %v", err)
	}

	// Save the token for future use
	saveToken(tokFile, tok)
	return tok, nil
}

func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

func saveToken(file string, token *oauth2.Token) {
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
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

func SendEmail(recipientAddress string, senderAddress string, senderPassword string, body string) error {
	from := senderAddress
	password := senderPassword
	to := []string{recipientAddress}
	smtpHost := "smtp.gmail.com"
	smtpPort := "587"
	dateString := time.Now().Format("2006-01-02")
	subject := "Journal Reminder for " + dateString

	headers := make(map[string]string)
	headers["From"] = from
	headers["To"] = strings.Join(to, ",")
	headers["Subject"] = subject

	message := ""
	for k, v := range headers {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + body
	messageByte := []byte(message)

	// Create authentication
	auth := smtp.PlainAuth("", from, password, smtpHost)

	// Send actual message
	err := smtp.SendMail(smtpHost+":"+smtpPort, auth, from, to, messageByte)
	if err != nil {
		fmt.Println("Send Email failed.")
		return err
	}
	return nil
}

func GetGoogleClient() (*docs.Service, error) {
	// Read credentials from file
	creds, err := ioutil.ReadFile("../../credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// Set up the OAuth2 config
	config, err := google.ConfigFromJSON(creds, docs.DocumentsScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}

	// Read or obtain the token
	token, err := getToken(config)
	if err != nil {
		log.Fatalf("Unable to get token: %v", err)
	}

	// Set up the token source
	ts := config.TokenSource(context.Background(), token)
	return docs.NewService(context.Background(), option.WithHTTPClient(oauth2.NewClient(context.Background(), ts)))
}

func RefreshGoogleAuth() (authCode string) {
	client_id := os.Getenv("GCP_CLIENT_ID")
	client_secret := os.Getenv("GCP_CLIENT_SECRET")
	client_auth_code := os.Getenv("GCP_AUTH_CODE")

	url := "https://accounts.google.com/o/oauth2/token"

	fmt.Println(client_id, client_secret, client_auth_code, url)

	authCode = ""

	authCode = os.Getenv("GCP_AUTH_CODE")

	return authCode
}

func getDocsService() (*docs.Service, error) {
	keyFilePath := "cmd/aws-lambda/credentials.json"
	keyData, err := ioutil.ReadFile(keyFilePath)
	if err != nil {
		return nil, err
	}

	conf, err := google.JWTConfigFromJSON(keyData, docs.DocumentsScope)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	client := conf.Client(ctx)
	docsService, err := docs.New(client)
	if err != nil {
		return nil, err
	}

	return docsService, nil
}
