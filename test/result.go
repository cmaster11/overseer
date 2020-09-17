package test

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/cmaster11/overseer/utils"
)

// Result contains a single test result
type Result struct {
	Input  string `json:"input"`
	Target string `json:"target"`
	Time   int64  `json:"time"`
	Type   string `json:"type"`
	Tag    string `json:"tag"`

	// If not nil, test has failed
	Error *string `json:"error"`

	// Result details
	Details *string `json:"details"`

	// If true, this alert is a duplicate of an ongoing alert
	IsDedup bool `json:"isDedup"`

	// If not nil, it means this error got triggered after a certain min-duration
	FirstErrorTime *int64 `json:"firstErrorTime"`

	// If true, this alert has recovered from a previous error
	Recovered bool `json:"recovered"`

	// It not nil, will be used as hash for this test
	UniqueHash *string `json:"uniqueHash"`

	// If not nil, describes result with a custom label
	TestLabel *string `json:"testLabel"`
}

// Hash generates a unique identifier for the original test (e.g. to deduplicate same results)
func (result *Result) Hash() string {
	if result.UniqueHash != nil {
		return utils.GetMD5Hash(*result.UniqueHash)
	}

	return utils.GetMD5Hash(result.Input + result.Target + result.Type + result.Tag)
}

// ResultFromJSON creates a result struct from a JSON payload
func ResultFromJSON(msg []byte) (*Result, error) {
	testResult := new(Result)

	if err := json.Unmarshal(msg, testResult); err != nil {
		// Is this old-overseer message type?
		data := map[string]string{}

		if err = json.Unmarshal(msg, &data); err != nil {
			return nil, err
		}

		if timeStr, ok := data["time"]; ok {
			timeInt, errConv := strconv.ParseInt(timeStr, 10, 64)
			if errConv != nil {
				return nil, errConv
			}

			resultStr := data["result"]
			errorStr := data["error"]
			var errorPtr *string
			if resultStr == "failed" {
				errorPtr = &errorStr
			}

			return &Result{
				Input:  data["input"],
				Target: data["target"],
				Time:   timeInt,
				Type:   data["type"],
				Tag:    data["tag"],
				Error:  errorPtr,
			}, nil
		}

		return nil, errors.New("failed to parse test result entry")
	}

	return testResult, nil
}
