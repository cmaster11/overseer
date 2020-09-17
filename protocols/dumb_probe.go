// Dumb probe, for internal Overseer testing

package protocols

import (
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/cmaster11/overseer/test"
	// Import all auth methods k8s
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

// DumbTest is our object.
type DumbTest struct {
	internalCounter uint
}

// Arguments returns the names of arguments which this protocol-test
// understands, along with corresponding regular-expressions to validate
// their values.
func (s *DumbTest) Arguments() map[string]string {
	known := map[string]string{
		"dumb-duration-min": `^[+]?([0-9]*(\.[0-9]*)?[a-z]+)+$`,
		"dumb-duration-max": `^[+]?([0-9]*(\.[0-9]*)?[a-z]+)+$`,
		// Fails at specific counter index
		// Counter is reset on failure
		"fail-at": `^\d+$`,
	}
	return known
}

// ShouldResolveHostname returns if this protocol requires the hostname resolution of the first test argument
func (s *DumbTest) ShouldResolveHostname() bool {
	return false
}

// Example returns sample usage-instructions for self-documentation purposes.
func (s *DumbTest) Example() string {
	str := `
Dumb Tester
-------------
Performs a test of random duration and result.

	fake-name must run dumb-test with dumb-duration-min 2s with dumb-duration-max 10s

If fail-at is defined, fails only at the specified counter index, then resets counter.
`
	return str
}

// RunTest is the part of our API which is invoked to actually execute a
// test against the given target.
func (s *DumbTest) RunTest(tst test.Test, target string, opts test.Options) error {
	var err error

	durationMin := 0 * time.Second
	durationMax := 5 * time.Second
	failAtCounter := -1

	if tst.Arguments["dumb-duration-min"] != "" {
		durationMin, err = time.ParseDuration(tst.Arguments["dumb-duration-min"])
		if err != nil {
			return err
		}
		if durationMin < 0 {
			return fmt.Errorf("dumb-duration-min must be > 0")
		}
	}
	if tst.Arguments["dumb-duration-max"] != "" {
		durationMax, err = time.ParseDuration(tst.Arguments["dumb-duration-max"])
		if err != nil {
			return err
		}
		if durationMax < 0 {
			return fmt.Errorf("dumb-duration-max must be > 0")
		}
	}
	if tst.Arguments["fail-at"] != "" {
		failAtUint64, errParse := strconv.ParseUint(tst.Arguments["fail-at"], 10, 32)
		if errParse != nil {
			return errParse
		}
		failAtCounter = int(failAtUint64)
	}

	if durationMax < durationMin {
		return errors.New("dumb-duration-max must be >= than dumb-duration-min")
	}

	fail := rand.Float64() >= 0.5
	if failAtCounter >= 0 {
		fail = s.internalCounter >= uint(failAtCounter)
		if fail {
			// Reset
			s.internalCounter = 0
		} else {
			s.internalCounter++
		}
	}

	diffDuration := int64(durationMax - durationMin)
	var randDiffDuration int64 = 0
	if diffDuration > 0 {
		randDiffDuration = rand.Int63n(diffDuration)
	}
	waitFor := time.Duration(randDiffDuration + int64(durationMin))

	time.Sleep(waitFor)

	if fail {
		return fmt.Errorf("dumb test failed (duration %s)", waitFor.String())
	}

	return nil
}

func (s *DumbTest) GetUniqueHashForTest(tst test.Test, opts test.Options) *string {
	return &tst.Target
}

//
// Register our protocol-tester.
//
func init() {
	Register("dumb-test", func() ProtocolTest {
		return &DumbTest{}
	})
}
