// Worker
//
// The worker sub-command executes tests pulled from a central redis queue.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-redis/redis"
	"github.com/google/subcommands"
	_ "github.com/skx/golang-metrics"
	"github.com/skx/overseer/parser"
	"github.com/skx/overseer/protocols"
	"github.com/skx/overseer/test"
)

// This is our structure, largely populated by command-line arguments
type workerCmd struct {
	// Should we run tests against IPv4 addresses?
	IPv4 bool

	// Should we run tests against IPv6 addresses?
	IPv6 bool

	// Should we retry failed tests a number of times to smooth failures?
	Retry bool

	// If we should retry failed tests, how many times before we give up?
	RetryCount int

	// Prior to retrying a failed test how long should we pause?
	RetryDelay time.Duration

	// The redis-host we're going to connect to for our queues.
	RedisHost string

	// The redis-database we're going to use.
	RedisDB int

	// The (optional) redis-password we'll use.
	RedisPassword string

	// How long should tests run for?
	Timeout time.Duration

	// Should the testing, and the tests, be verbose?
	Verbose bool

	// The handle to our redis-server
	_r *redis.Client
}

//
// Glue
//
func (*workerCmd) Name() string     { return "worker" }
func (*workerCmd) Synopsis() string { return "Fetch jobs from the central queue and execute them" }
func (*workerCmd) Usage() string {
	return `worker :
  Execute tests pulled from the central redis queue, until terminated.
`
}

// verbose shows a message only if we're running verbosely
func (p *workerCmd) verbose(txt string) {
	if p.Verbose {
		fmt.Printf(txt)
	}
}

//
// Flag setup.
//
func (p *workerCmd) SetFlags(f *flag.FlagSet) {

	//
	// Setup the default options here, these can be loaded/replaced
	// via a configuration-file if it is present.
	//
	var defaults workerCmd
	defaults.IPv4 = true
	defaults.IPv6 = true
	defaults.Retry = true
	defaults.RetryCount = 5
	defaults.RetryDelay = 5 * time.Second
	defaults.Timeout = 10 * time.Second
	defaults.Verbose = false
	defaults.RedisHost = "localhost:6379"
	defaults.RedisDB = 0
	defaults.RedisPassword = ""

	//
	// If we have a configuration file then load it
	//
	if len(os.Getenv("OVERSEER")) > 0 {
		cfg, err := ioutil.ReadFile(os.Getenv("OVERSEER"))
		if err == nil {
			err = json.Unmarshal(cfg, &defaults)
			if err != nil {
				fmt.Printf("WARNING: Error loading overseer.json - %s\n",
					err.Error())
			}
		} else {
			fmt.Printf("WARNING: Failed to read configuration-file - %s\n",
				err.Error())
		}
	}

	//
	// Allow these defaults to be changed by command-line flags
	//
	// Verbose
	f.BoolVar(&p.Verbose, "verbose", defaults.Verbose, "Show more output.")

	// Protocols
	f.BoolVar(&p.IPv4, "4", defaults.IPv4, "Enable IPv4 tests.")
	f.BoolVar(&p.IPv6, "6", defaults.IPv6, "Enable IPv6 tests.")

	// Timeout
	f.DurationVar(&p.Timeout, "timeout", defaults.Timeout, "The global timeout for all tests, in seconds.")

	// Retry
	f.BoolVar(&p.Retry, "retry", defaults.Retry, "Should failing tests be retried a few times before raising a notification.")
	f.IntVar(&p.RetryCount, "retry-count", defaults.RetryCount, "How many times to retry a test, before regarding it as a failure.")
	f.DurationVar(&p.RetryDelay, "retry-delay", defaults.RetryDelay, "The time to sleep between failing tests.")

	// Redis
	f.StringVar(&p.RedisHost, "redis-host", defaults.RedisHost, "Specify the address of the redis queue.")
	f.IntVar(&p.RedisDB, "redis-db", defaults.RedisDB, "Specify the database-number for redis.")
	f.StringVar(&p.RedisPassword, "redis-pass", defaults.RedisPassword, "Specify the password for the redis queue.")
}

// notify is used to store the result of a test in our redis queue.
//
func (p *workerCmd) notify(test test.Test, result error) error {

	//
	// If we don't have a redis-server then return immediately.
	//
	// (This shouldn't happen, as without a redis-handle we can't
	// fetch jobs to execute.)
	//
	if p._r == nil {
		return nil
	}

	//
	// The message we'll publish will be a JSON hash
	//
	msg := map[string]string{
		"input":  test.Input,
		"result": "passed",
		"target": test.Target,
		"time":   fmt.Sprintf("%d", time.Now().Unix()),
		"type":   test.Type,
	}

	//
	// Was the test result a failure?  If so update the object
	// to contain the failure-message, and record that it was
	// a failure rather than the default pass.
	//
	if result != nil {
		msg["result"] = "failed"
		msg["error"] = result.Error()
	}

	//
	// Convert the MAP to a JSON string we can notify.
	//
	j, err := json.Marshal(msg)
	if err != nil {
		fmt.Printf("Failed to encode test-result to JSON: %s", err.Error())
		return err
	}

	//
	// Publish the message to the queue.
	//
	_, err = p._r.RPush("overseer.results", j).Result()
	if err != nil {
		fmt.Printf("Result addition failed: %s\n", err)
		return err
	}

	return nil
}

// runTest is really the core of our application, as it is responsible
// for receiving a test to execute, executing it, and then issuing
// the notification with the result.
func (p *workerCmd) runTest(tst test.Test, opts test.TestOptions) error {

	//
	// Setup our local state.
	//
	testType := tst.Type
	testTarget := tst.Target

	//
	// Look for a suitable protocol handler
	//
	tmp := protocols.ProtocolHandler(testType)

	//
	// Each test will be executed for each address-family, so we need to
	// keep track of the IPs of the real test-target.
	//
	var targets []string

	//
	// If the first argument looks like an URI then get the host
	// out of it.
	//
	if strings.Contains(testTarget, "://") {
		u, err := url.Parse(testTarget)
		if err != nil {
			return err
		}
		testTarget = u.Hostname()
	}

	//
	// Now resolve the target to IPv4 & IPv6 addresses.
	//
	ips, err := net.LookupIP(testTarget)
	if err != nil {

		//
		// Notify the world about our DNS-failure.
		//
		p.notify(tst, fmt.Errorf("Failed to resolve name %s", testTarget))

		//
		// Otherwise we're done.
		//
		fmt.Printf("WARNING: Failed to resolve %s for %s test!\n", testTarget, testType)
		return err
	}

	//
	// We'll now run the test against each of the resulting IPv4 and
	// IPv6 addresess - ignoring any IP-protocol which is disabled.
	//
	for _, ip := range ips {
		if ip.To4() != nil {
			if p.IPv4 {
				targets = append(targets, ip.String())
			}
		}
		if ip.To16() != nil && ip.To4() == nil {
			if p.IPv6 {
				targets = append(targets, ip.String())
			}
		}
	}

	//
	// Now for each target, run the test.
	//
	for _, target := range targets {

		//
		// Show what we're doing.
		//
		p.verbose(fmt.Sprintf("Running '%s' test against %s (%s)\n", testType, testTarget, target))

		//
		// We'll repeat failing tests up to five times by default
		//
		attempt := 0
		maxAttempts := p.RetryCount

		//
		// If retrying is disabled then don't retry.
		//
		if p.Retry == false {
			maxAttempts = attempt + 1
		}

		//
		// The result of the test.
		//
		var result error

		//
		// Prepare to repeat the test.
		//
		// We only repeat tests that fail, if the test passes then
		// it will only be executed once.
		//
		// This is designed to cope with transient failures, at a
		// cost that flapping services might be missed.
		//
		for attempt < maxAttempts {
			attempt += 1

			//
			// Run the test
			//
			result = tmp.RunTest(tst, target, opts)

			//
			// If the test passed then we're good.
			//
			if result == nil {
				p.verbose(fmt.Sprintf("\t[%d/%d] - Test passed.\n", attempt, maxAttempts))

				// break out of loop
				attempt = maxAttempts + 1

			} else {

				//
				// The test failed.
				//
				// It will be repeated before a notifier
				// is invoked.
				//
				p.verbose(fmt.Sprintf("\t[%d/%d] Test failed: %s\n", attempt, maxAttempts, result.Error()))

				//
				// Sleep before retrying the failing test.
				//
				p.verbose(fmt.Sprintf("\t\tSleeping for %s before retrying\n", p.RetryDelay.String()))
				time.Sleep(p.RetryDelay)
			}
		}

		//
		// Post the result of the test to the notifier.
		//
		// Before we trigger the notification we need to
		// update the target to the thing we probed, which might
		// not necessarily be that which was originally submitted.
		//
		//  i.e. "mail.steve.org.uk must run ssh" might become
		// "1.2.3.4 must run ssh" as a result of the DNS lookup.
		//
		// However because we might run the same test against
		// multiple hosts we need to do this with a copy so that
		// we don't lose the original target.
		//
		copy := tst
		copy.Target = target

		//
		// We also want to filter out any password which was found
		// on the input-line.
		//
		copy.Input = tst.Sanitize()

		//
		// Now we can trigger the notification with our updated
		// copy of the test.
		//
		p.notify(copy, result)
	}

	return nil
}

//
// Entry-point.
//
func (p *workerCmd) Execute(_ context.Context, f *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {

	//
	// Connect to the redis-host.
	//
	p._r = redis.NewClient(&redis.Options{
		Addr:     p.RedisHost,
		Password: p.RedisPassword,
		DB:       p.RedisDB,
	})

	//
	// And run a ping, just to make sure it worked.
	//
	_, err := p._r.Ping().Result()
	if err != nil {
		fmt.Printf("Redis connection failed: %s\n", err.Error())
		return subcommands.ExitFailure
	}

	//
	// Setup the options passed to each test, by copying our
	// global ones.
	//
	var opts test.TestOptions
	opts.Verbose = p.Verbose
	opts.Timeout = p.Timeout

	//
	// Create a parser for our input
	//
	parse := parser.New()

	//
	// Wait for jobs, in a blocking-manner.
	//
	for true {

		//
		// Get a job.
		//
		test, _ := p._r.BLPop(0, "overseer.jobs").Result()

		//
		// Parse it
		//
		//   test[0] will be "overseer.jobs"
		//
		//   test[1] will be the value removed from the list.
		//
		if len(test) >= 1 {
			job, err := parse.ParseLine(test[1], nil)

			if err == nil {
				p.runTest(job, opts)
			} else {
				fmt.Printf("Error parsing job from queue: %s - %s\n", test[1], err.Error())
			}
		}

	}

	return subcommands.ExitSuccess
}
