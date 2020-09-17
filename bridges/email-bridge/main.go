//
// This is the email bridge, which should be built like so:
//
//     go build .
//
// Once built launch it as follows:
//
//     $ ./email-bridge -email=sysadmin@example.com,hello@gmail.com
//
// When a test fails an email will sent via SMTP
//
// Alberto
// --
//

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/cmaster11/overseer/test"
	"github.com/cmaster11/overseer/utils"

	"github.com/go-redis/redis"
)

// TemplateSubject is our text/template which is used to generate the email
// subject to the user.
var TemplateSubject = template.Must(template.New("tmpl").Parse(strings.TrimSpace(`
Overseer [
{{- if .error -}}
	ERR
	{{- if .isDedup -}}
	-DUP
	{{- end -}}
{{- else -}}
	{{- if .recovered -}}
	RECOVERED
	{{- else -}}
	OK
	{{- end -}}
{{- end -}}
]
{{- if .tag}} ({{.tag}}){{- end -}}
: {{if .testLabel}}{{.testLabel}}{{else}}{{.input}}{{end}} ({{.date}})
`)))

// TemplateBody is our text/template which is used to generate the email
// notification to the user.
var TemplateBody = template.Must(template.New("tmpl").Parse(strings.TrimSpace(`
Overseer: 
{{- if .error }} Error
{{- if .isDedup}} (duplicated){{end -}}
: {{.error}}
{{- else -}}
{{- if .recovered }} Test recovered
{{- else }} Test ok
{{- end -}}
{{- end}}

{{- if .details}}
Details: {{.details}}
{{- end}}

Tag: {{if .tag}}{{.tag}}{{else}}None{{end}}
{{- if .testLabel}}
Test label: {{.testLabel}}
{{- end}}
Input: {{.input}}

Target: {{ .target }}
Type: {{ .type }}
Time: {{ .date }}
{{- if .firstErrorTimeDate}}
First error time: {{.firstErrorTimeDate}}
{{- end}}
`)))

type EmailBridge struct {
	Sender *utils.EmailSender

	// The email we notify
	Emails []string

	SendTestSuccess   bool
	SendTestRecovered bool
}

func getTemplateMapFromTestResult(testResult *test.Result) map[string]interface{} {
	firstErrorTimeDate := ""
	if testResult.FirstErrorTime != nil {
		firstErrorTimeDate = time.Unix(*testResult.FirstErrorTime, 0).UTC().String()
	}

	return map[string]interface{}{
		"error":              testResult.Error,
		"isDedup":            testResult.IsDedup,
		"recovered":          testResult.Recovered,
		"tag":                testResult.Tag,
		"target":             testResult.Target,
		"input":              testResult.Input,
		"type":               testResult.Type,
		"date":               time.Unix(testResult.Time, 0).UTC().String(),
		"firstErrorTimeDate": firstErrorTimeDate,
		"details":            testResult.Details,
		"testLabel":          testResult.TestLabel,
	}
}

//
// Given a JSON string decode it and post it via email if it describes
// a test-failure.
//
func (bridge *EmailBridge) Process(msg []byte) {
	testResult, err := test.ResultFromJSON(msg)
	if err != nil {
		panic(err)
	}

	// If the test passed then we don't care, unless otherwise defined
	shouldSend := true
	if testResult.Error == nil {
		shouldSend = false

		if bridge.SendTestSuccess {
			shouldSend = true
		}

		if bridge.SendTestRecovered && testResult.Recovered {
			shouldSend = true
		}
	}

	if !shouldSend {
		return
	}

	fmt.Printf("Processing result: %+v\n", testResult)

	templateMap := getTemplateMapFromTestResult(testResult)

	//
	// Render our template into a buffer.
	//
	var subject, body string

	{
		buf := &bytes.Buffer{}
		err = TemplateSubject.Execute(buf, templateMap)
		if err != nil {
			fmt.Printf("Failed to compile email-template subject %s\n", err.Error())
			return
		}

		subject = buf.String()
	}

	{
		buf := &bytes.Buffer{}
		err = TemplateBody.Execute(buf, templateMap)
		if err != nil {
			fmt.Printf("Failed to compile email-template body %s\n", err.Error())
			return
		}

		body = buf.String()
	}

	// Prepare email to send
	message := bridge.Sender.WritePlainEmail(bridge.Emails, subject, body)

	err = bridge.Sender.SendRawMail(bridge.Emails, message)

	if err != nil {
		fmt.Printf("Waiting for process to terminate failed: %s\n", err.Error())
	}
}

//
// Entry Point
//
func main() {

	//
	// Parse our flags
	//
	redisHost := flag.String("redis-host", "127.0.0.1:6379", "Specify the address of the redis queue.")
	redisPass := flag.String("redis-pass", "", "Specify the password of the redis queue.")
	redisQueueKey := flag.String("redis-queue-key", "overseer.results", "Specify the redis queue key to use.")

	smtpHost := flag.String("smtp-host", "smtp.gmail.com", "The SMTP host")
	smtpPort := flag.Uint("smtp-port", 587, "The SMTP port")
	smtpUsername := flag.String("smtp-username", "", "The SMTP username")
	smtpPassword := flag.String("smtp-password", "", "The SMTP password")

	emailStr := flag.String("email", "", "The email addresses to notify, separated by comma")
	sendTestSuccess := flag.Bool("send-test-success", false, "Send also test results when successful")
	sendTestRecovered := flag.Bool("send-test-recovered", false, "Send also test results when a test recovers from failure (valid only when used together with deduplication rules)")

	flag.Parse()

	emailSender := utils.NewEmailSender(*smtpHost, *smtpPort, *smtpUsername, *smtpPassword)

	emailsSplit := strings.Split(*emailStr, ",")
	var emailsValid []string
	for _, email := range emailsSplit {
		if email == "" {
			continue
		}

		emailsValid = append(emailsValid, email)
	}

	//
	// Sanity-check.
	//
	if len(emailsValid) == 0 {
		fmt.Printf("Usage: email-bridge -email=sysadmin@example.com [-redis-host=127.0.0.1:6379] [-redis-pass=foo]\n")
		os.Exit(1)
	}

	//
	// Create the redis client
	//
	r := redis.NewClient(&redis.Options{
		Addr:     *redisHost,
		Password: *redisPass,
		DB:       0, // use default DB
	})

	//
	// And run a ping, just to make sure it worked.
	//
	_, err := r.Ping().Result()
	if err != nil {
		fmt.Printf("Redis connection failed: %s\n", err.Error())
		os.Exit(1)
	}

	bridge := EmailBridge{
		Sender:            emailSender,
		Emails:            emailsValid,
		SendTestRecovered: *sendTestRecovered,
		SendTestSuccess:   *sendTestSuccess,
	}

	for {

		//
		// Get test-results
		//
		msg, _ := r.BLPop(0, *redisQueueKey).Result()

		//
		// If they were non-empty, process them.
		//
		//   msg[0] will be "overseer.results"
		//
		//   msg[1] will be the value removed from the list.
		//
		if len(msg) >= 1 {
			bridge.Process([]byte(msg[1]))
		}
	}
}
