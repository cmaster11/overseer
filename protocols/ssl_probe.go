// SSL Tester
//
// The SSL tester allows you to confirm that a remote TCP-server is
// responding with a correctly setup SSL certificate.
//
// This test is invoked via input like so:
//
//    example.com must run ssl
//
// By default tests will fail if you're probing an SSL-site which has
// a certificate which will expire within the next 14 days. To change
// the time-period specify it explicitly like so, if not stated the
// expiration period is assumed to be days:
//
//    # seven days
//    steve.fi must run ssl with expiration 7d
//
//    # 12 hours (!)
//    steve.fi must run ssl with expiration 12h
//

package protocols

import (
	"crypto/tls"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cmaster11/overseer/test"
)

// SSLTest is our object.
type SSLTest struct {
}

// Arguments returns the names of arguments which this protocol-test
// understands, along with corresponding regular-expressions to validate
// their values.
func (s *SSLTest) Arguments() map[string]string {
	known := map[string]string{
		"expiration": "^([0-9]+[hd]?)$",
	}
	return known
}

// ShouldResolveHostname returns if this protocol requires the hostname resolution of the first test argument
func (s *SSLTest) ShouldResolveHostname() bool {
	return true
}

// Example returns sample usage-instructions for self-documentation purposes.
func (s *SSLTest) Example() string {
	str := `
SSL Tester
-----------
The SSL tester allows you to confirm that a remote TCP-server is
responding with a correctly setup SSL certificate.

This test is invoked via input like so:

   example.com must run ssl

By default tests will fail if you're probing an SSL-site which has
a certificate which will expire within the next 14 days. To change
the time-period specify it explicitly like so, if not stated the
expiration period is assumed to be days:

   # seven days
   steve.fi must run ssl with expiration 7d

   # 12 hours (!)
   steve.fi must run ssl with expiration 12h
`
	return str
}

// RunTest is the part of our API which is invoked to actually execute a
// SSL-test against the given URL.
//
// For the purposes of clarity this test makes a TCP dial and verifies SSL
// certificates validity. The `test.Test` structure contains our raw test,
// and the `target` variable contains the IP address against which to make
// the request.
//
// So:
//
//    tst.Target => "steve.kemp.fi
//
//    target => "176.9.183.100"
//
func (s *SSLTest) RunTest(tst test.Test, _ string, opts test.Options) error {

	var err error
	target := tst.Target

	//
	// The default expiration-time 14 days.
	//
	period := 14 * 24

	//
	// The user might have specified a different period
	// in hours / days.
	//
	expire := tst.Arguments["expiration"]
	if expire != "" {

		//
		// How much to scale the given figure by
		//
		// By default if no units are specified we'll
		// assume the figure is in days, so no scaling
		// is required.
		//
		mul := 1

		// Days?
		if strings.HasSuffix(expire, "d") {
			expire = strings.Replace(expire, "d", "", -1)
			mul = 24
		}

		// Hours?
		if strings.HasSuffix(expire, "h") {
			expire = strings.Replace(expire, "h", "", -1)
			mul = 1
		}

		// Get the period.
		period, err = strconv.Atoi(expire)
		if err != nil {
			return err
		}

		//
		// Multiply by our multiplier.
		//
		period *= mul
	}

	//
	// Check the expiration
	//
	hours, err := s.SSLExpiration(target, opts.Verbose)

	if err == nil {
		// Is the age too short?
		if int64(hours) < int64(period) {

			return fmt.Errorf("SSL certificate will expire in %d hours (%d days)", hours, int(hours/24))
		}
	}

	//
	// If we reached here all is OK
	//
	return nil
}

// SSLExpiration returns the number of hours remaining for a given
// SSL certificate chain.
func (s *SSLTest) SSLExpiration(host string, verbose bool) (int64, error) {

	// Expiry time, in hours
	var hours int64
	hours = -1

	//
	// If no port is specified default to :443
	//
	p := strings.Index(host, ":")
	if p == -1 {
		host += ":443"
	}

	//
	// Show what we're doing.
	//
	if verbose {
		fmt.Printf("SSLExpiration testing: %s\n", host)
	}

	cfg := &tls.Config{}

	conn, err := tls.Dial("tcp", host, cfg)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	timeNow := time.Now()
	for _, chain := range conn.ConnectionState().VerifiedChains {
		for _, cert := range chain {

			// Get the expiration time, in hours.
			expiresIn := int64(cert.NotAfter.Sub(timeNow).Hours())

			if verbose {
				fmt.Printf("SSLExpiration - certificate: %s expires in %d hours (%d days)\n", cert.Subject.CommonName, expiresIn, expiresIn/24)
			}

			// If we've not checked anything this is the benchmark
			if hours == -1 {
				hours = expiresIn
			} else {
				// Otherwise replace our result if the
				// certificate is going to expire more
				// recently than the current "winner".
				if expiresIn < hours {
					hours = expiresIn
				}
			}
		}
	}

	return hours, nil
}

func (s *SSLTest) GetUniqueHashForTest(tst test.Test, opts test.Options) *string {
	return nil
}

// init is used to dynamically register our protocol-tester.
func init() {
	Register("ssl", func() ProtocolTest {
		return &SSLTest{}
	})
}
