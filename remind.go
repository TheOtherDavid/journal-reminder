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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ssm"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	docs "google.golang.org/api/docs/v1"
	"google.golang.org/api/option"
)

// dateRe extracts a M/D/YYYY or D/M/YYYY date from heading text, ignoring any
// surrounding characters (weekday suffixes, colons, or non-printing characters
// such as zero-width spaces that Google Docs can insert).
var dateRe = regexp.MustCompile(`\d{1,2}/\d{1,2}/\d{4}`)

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

		year, ok := yearFromTitle(doc.Title)
		if !ok {
			fmt.Printf("Could not determine year from title %q; skipping document.\n", doc.Title)
			continue
		}
		currentTime := time.Now()
		targetDate := time.Date(year, currentTime.Month(), currentTime.Day(), 0, 0, 0, 0, time.UTC)

		message += extractEntryForDate(doc, targetDate)
	}
	//Once we have the appropriate journal entries, compose them into an e-mail message
	fmt.Println(message)

	//TODO: Send the e-mail to my personal account, using my bot e-mail account.
	senderEmail := os.Getenv("SENDER_EMAIL")
	recipientEmail := os.Getenv("RECIPIENT_EMAIL")
	if senderEmail == "" {
		return errors.New("SENDER_EMAIL env variable not set")
	}
	if recipientEmail == "" {
		return errors.New("RECIPIENT_EMAIL env variable not set")
	}
	senderPassword, err := getSenderPassword()
	if err != nil {
		return fmt.Errorf("unable to get sender password: %w", err)
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

// titleYearRe pulls the 4-digit year out of a journal title like "Journal 2021".
var titleYearRe = regexp.MustCompile(`\d{4}`)

// yearFromTitle extracts the 4-digit year from a journal document title.
// Returns ok=false if the title contains no 4-digit year.
func yearFromTitle(title string) (int, bool) {
	m := titleYearRe.FindString(title)
	if m == "" {
		return 0, false
	}
	y, err := strconv.Atoi(m)
	if err != nil {
		return 0, false
	}
	return y, true
}

// parseHeadingDate extracts and parses a M/D/YYYY (US) or D/M/YYYY (European)
// date from heading text. It tolerates surrounding text and non-printing
// characters (e.g. zero-width spaces) because it matches the date with a regex
// rather than relying on the exact heading format. US ordering is tried first.
// Returns ok=false when no parseable date is present.
func parseHeadingDate(text string) (time.Time, bool) {
	dateString := dateRe.FindString(text)
	if dateString == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse("1/2/2006", dateString); err == nil {
		return t, true
	}
	if t, err := time.Parse("2/1/2006", dateString); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// extractEntryForDate scans a journal document for the HEADING_4 entry whose
// date equals target, and returns the text of that entry: the matching heading
// line plus the paragraphs that follow it, up to (but not including) the next
// HEADING_4 or a page break. Returns "" when there is no entry for target.
func extractEntryForDate(doc *docs.Document, target time.Time) string {
	if doc == nil || doc.Body == nil {
		return ""
	}
	message := ""
	appendMode := false

	for _, contentItem := range doc.Body.Content {
		if contentItem.Paragraph == nil {
			continue
		}
		style := contentItem.Paragraph.ParagraphStyle
		isHeading := style != nil && style.NamedStyleType == "HEADING_4"

		if isHeading {
			if appendMode {
				// Reached the next entry's heading: stop.
				break
			}
			// Not yet collecting: does this heading match the target date?
			if len(contentItem.Paragraph.Elements) > 0 {
				el := contentItem.Paragraph.Elements[0]
				if el != nil && el.TextRun != nil {
					text := el.TextRun.Content
					if text != "" && text != "\n" {
						if headingDate, ok := parseHeadingDate(text); ok && headingDate.Equal(target) {
							appendMode = true
						}
					}
				}
			}
		}

		if appendMode {
			for _, el := range contentItem.Paragraph.Elements {
				if el == nil {
					continue
				}
				if el.PageBreak != nil {
					// A page break terminates the entry.
					return message
				}
				if el.TextRun != nil {
					message += el.TextRun.Content
				}
			}
		}
	}
	return message
}

// getSenderPassword returns the SMTP sender password. It prefers an explicit
// SENDER_PASSWORD env var (handy for local CLI runs) and otherwise reads the
// SecureString SSM parameter named by SENDER_PASSWORD_SSM_PARAM. The secret is
// therefore never stored in the function's environment or in IaC state.
func getSenderPassword() (string, error) {
	if v := os.Getenv("SENDER_PASSWORD"); v != "" {
		return v, nil
	}
	name := os.Getenv("SENDER_PASSWORD_SSM_PARAM")
	if name == "" {
		return "", errors.New("neither SENDER_PASSWORD nor SENDER_PASSWORD_SSM_PARAM is set")
	}
	sess, err := session.NewSession()
	if err != nil {
		return "", err
	}
	out, err := ssm.New(sess).GetParameter(&ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", err
	}
	return aws.StringValue(out.Parameter.Value), nil
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
