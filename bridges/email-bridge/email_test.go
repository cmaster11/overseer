package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/cmaster11/overseer/test"
)

func TestEmailTemplate(t *testing.T) {

	errString := "an error!"
	detailString := "blablabla!"
	testLabelString := "My label"
	firstErrorTime := time.Now().Unix() - 30

	templateMap := getTemplateMapFromTestResult(&test.Result{
		Input:          "asasd",
		Target:         "1234",
		Time:           time.Now().Unix(),
		Type:           "my-type",
		Tag:            "my-tag",
		Error:          &errString,
		Details:        &detailString,
		IsDedup:        true,
		Recovered:      false,
		UniqueHash:     nil,
		TestLabel:      &testLabelString,
		FirstErrorTime: &firstErrorTime,
	})

	buf := &bytes.Buffer{}
	err := TemplateSubject.Execute(buf, templateMap)
	if err != nil {
		t.Errorf("failed to execute subject template: %+v", err)
		t.Failed()
	}

	t.Logf("email subject: %s", buf.String())

	buf = &bytes.Buffer{}
	err = TemplateBody.Execute(buf, templateMap)
	if err != nil {
		t.Errorf("failed to execute body template: %+v", err)
		t.Failed()
	}

	t.Logf("email body:\n%s", buf.String())
}
