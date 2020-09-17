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
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cmaster11/overseer/parser"
	"github.com/cmaster11/overseer/protocols"
	"github.com/cmaster11/overseer/test"
	"github.com/cmaster11/overseer/utils"
	"github.com/go-redis/redis"
	"github.com/google/subcommands"
	"github.com/marpaia/graphite-golang"
	_ "github.com/skx/golang-metrics"
)

// This is our structure, largely populated by command-line arguments
type workerCmd struct {
	// How many parallel checks can we execute?
	Parallel uint

	// Should we run tests against IPv4 addresses?
	IPv4 bool

	// Should we run tests against IPv6 addresses?
	IPv6 bool

	// Should we retry failed tests a number of times to smooth failures?
	Retry bool

	// If we should retry failed tests, how many times before we give up?
	RetryCount uint

	// Prior to retrying a failed test how long should we pause?
	RetryDelay time.Duration

	// Default min duration
	MinDuration time.Duration

	// Default min duration cache lifetime factor
	MinDurationCacheFactor uint

	// Default deduplication duration
	DedupDuration time.Duration

	// The redis-host we're going to connect to for our queues.
	RedisHost string

	// The redis-database we're going to use.
	RedisDB int

	// The (optional) redis-password we'll use.
	RedisPassword string

	// The redis-sockt we're going to use. (If used, we ignore the specified host / port)
	RedisSocket string

	// Redis connection timeout
	RedisDialTimeout time.Duration

	// Tag applied to all results
	Tag string

	// How long should tests run for?
	Timeout time.Duration

	// Should the testing, and the tests, be verbose?
	Verbose bool

	// Default period test sleep, if not overridden by specific test setting
	PeriodTestSleep time.Duration

	// Default period test threshold percentage, if not overridden by specific test setting
	PeriodTestThreshold float32

	// The handle to our redis-server
	_r *redis.Client

	// The handle to our graphite-server
	_g *graphite.Graphite
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

// MetricsFromEnvironment sets up a carbon connection from the environment
// if suitable values are found
func (p *workerCmd) MetricsFromEnvironment() {

	//
	// Get the hostname to connect to.
	//
	host := os.Getenv("METRICS_HOST")
	if host == "" {
		host = os.Getenv("METRICS")
	}

	// No host then we'll return
	if host == "" {
		return
	}

	// Split the into Host + Port
	ho, pr, err := net.SplitHostPort(host)
	if err != nil {
		// If that failed we assume the port was missing
		ho = host
		pr = "2003"
	}

	// Setup the protocol to use
	protocol := os.Getenv("METRICS_PROTOCOL")
	if protocol == "" {
		protocol = "udp"
	}

	// Ensure that the port is an integer
	port, err := strconv.Atoi(pr)
	if err == nil {
		p._g, err = graphite.GraphiteFactory(protocol, ho, port, "")

		if err != nil {
			fmt.Printf("Error setting up metrics - skipping - %s\n", err.Error())
		}
	} else {
		fmt.Printf("Error setting up metrics - failed to convert port to number - %s\n", err.Error())

	}
}

// verbose shows a message only if we're running verbosely
func (p *workerCmd) verbose(txt string) {
	if p.Verbose {
		fmt.Print(txt)
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
	defaults.Parallel = uint(runtime.NumCPU())
	defaults.IPv4 = true
	defaults.IPv6 = true
	defaults.Retry = true
	defaults.RetryCount = 5
	defaults.RetryDelay = 5 * time.Second
	defaults.MinDuration = 0
	defaults.MinDurationCacheFactor = 10
	defaults.DedupDuration = 0
	defaults.Tag = ""
	defaults.Timeout = 10 * time.Second
	defaults.Verbose = false
	defaults.RedisHost = "localhost:6379"
	defaults.RedisDB = 0
	defaults.RedisPassword = ""
	defaults.RedisDialTimeout = 5 * time.Second
	defaults.PeriodTestSleep = 5 * time.Second
	defaults.PeriodTestThreshold = 0

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
	// Worker
	f.UintVar(&p.Parallel, "parallel", defaults.Parallel, "Number of parallel tests the worker can be handled at the same time.")

	// Verbose
	f.BoolVar(&p.Verbose, "verbose", defaults.Verbose, "Show more output.")

	// Protocols
	f.BoolVar(&p.IPv4, "4", defaults.IPv4, "Enable IPv4 tests.")
	f.BoolVar(&p.IPv6, "6", defaults.IPv6, "Enable IPv6 tests.")

	// Timeout
	f.DurationVar(&p.Timeout, "timeout", defaults.Timeout, "The global timeout for all tests, in seconds.")

	// Retry
	f.BoolVar(&p.Retry, "retry", defaults.Retry, "Should failing tests be retried a few times before raising a notification.")
	f.UintVar(&p.RetryCount, "retry-count", defaults.RetryCount, "How many times to retry a test, before regarding it as a failure.")
	f.DurationVar(&p.RetryDelay, "retry-delay", defaults.RetryDelay, "The time to sleep between failing tests.")

	f.DurationVar(&p.DedupDuration, "dedup", defaults.DedupDuration, "The maximum duration of a deduplication.")
	f.DurationVar(&p.MinDuration, "min-duration", defaults.MinDuration, "The minimum duration of an error, for it to generate an alert.")
	f.UintVar(&p.MinDurationCacheFactor, "min-duration-cache-factor", defaults.MinDurationCacheFactor,
		"The lifetime factor for a min-duration error, for it to be reset (e.g. min-duration=2sec, min-duration-cache-factor=10 -> if an error is thrown after 20sec, it will be again considered like a first-time error).")

	// Redis
	f.StringVar(&p.RedisHost, "redis-host", defaults.RedisHost, "Specify the address of the redis queue.")
	f.IntVar(&p.RedisDB, "redis-db", defaults.RedisDB, "Specify the database-number for redis.")
	f.StringVar(&p.RedisPassword, "redis-pass", defaults.RedisPassword, "Specify the password for the redis queue.")
	f.StringVar(&p.RedisSocket, "redis-socket", defaults.RedisSocket, "If set, will be used for the redis connections.")
	f.DurationVar(&p.RedisDialTimeout, "redis-timeout", defaults.RedisDialTimeout, "Redis connection timeout.")

	// Tag
	f.StringVar(&p.Tag, "tag", defaults.Tag, "Specify the tag to add to all test-results.")

	// Period test
	f.DurationVar(&p.PeriodTestSleep, "period-test-sleep", defaults.PeriodTestSleep, "The sleeping interval between subsequent tests in a period-test.")
	f.Var(utils.NewPercentageValue(defaults.PeriodTestThreshold, &p.PeriodTestThreshold), "period-test-threshold", "The percentage of failures need to trigger an alert in a period-test.")
}

// notify is used to store the result of a test in our redis queue.
func (p *workerCmd) notify(testDefinition test.Test, uniqueHash *string, resultError error, details *string) error {

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
	testResult := &test.Result{
		Input:      testDefinition.Input,
		Target:     testDefinition.Target,
		Time:       time.Now().Unix(),
		Type:       testDefinition.Type,
		Tag:        p.Tag,
		Details:    details,
		UniqueHash: uniqueHash,
		TestLabel:  testDefinition.TestLabel,
	}

	//
	// Was the test result a failure?  If so update the object
	// to contain the failure-message, and record that it was
	// a failure rather than the default pass.
	//
	if resultError != nil {
		errorString := resultError.Error()
		testResult.Error = &errorString
	}

	now := time.Now()

	// If test has a min duration rule, avoid triggering a notification if not needed, or clean the min duration cache if needed.
	if testDefinition.MinDuration != nil {
		minDurationSeconds := int64(*testDefinition.MinDuration / time.Second)

		// We need a minimum cache duration, otherwise the min duration test cannot work
		if testDefinition.MinDurationCacheFactor == 0 {
			testDefinition.MinDurationCacheFactor = 2
		}

		hash := testResult.Hash()
		firstErrorTime := p.getMinDurationFirstErrorTime(hash)
		testResult.FirstErrorTime = firstErrorTime

		expireDuration := *testDefinition.MinDuration * time.Duration(testDefinition.MinDurationCacheFactor)

		if testResult.Error != nil {

			// With minDuration, we don't want to trigger the notification, unless the test has failed for a long enough amount of time
			if firstErrorTime != nil {
				diffFirstError := now.Unix() - *firstErrorTime

				// We want to keep the ORIGINAL error time as alert shown, just extend the expiry time
				p.setMinDurationFirstErrorTime(hash, *firstErrorTime, expireDuration)

				if diffFirstError < minDurationSeconds {
					// There is no need to trigger the notification, because not enough time has passed since the error got fired
					p.verbose(fmt.Sprintf("Skipping notification (minDuration, alert shown %s ago) for test `%s` (%s)\n",
						time.Duration(diffFirstError)*time.Second,
						testDefinition.Input, testDefinition.Target))
					return nil
				}

				// Let's show the alert!
				// Mark it as shown
				p.setMinDurationAlertShown(hash, true, expireDuration)

			} else {
				p.setMinDurationFirstErrorTime(hash, now.Unix(), expireDuration)

				// Do not throw alert here, because this is the first generated alert
				return nil
			}

		} else {
			// Check if there was any min duration cache

			if firstErrorTime != nil {
				diffFirstError := now.Unix() - *firstErrorTime

				alertShown := p.getMinDurationAlertShown(hash)

				// Clear the cache because test got resolved
				p.clearMinDurationFirstErrorTime(hash)
				p.clearMinDurationAlertShown(hash)
				testResult.Recovered = true

				// If the error has never been triggered, because min duration did not expire, do not trigger a recovered message
				if diffFirstError < minDurationSeconds {
					p.verbose(fmt.Sprintf("Test recovered (min duration not met, skipping recovered message): `%s` (%s)\n",
						testDefinition.Input, testDefinition.Target))
					return nil
				}

				// Check if we want to show the recovered message
				if !alertShown {
					p.verbose(fmt.Sprintf("Test recovered (alert not shown, skipping recovered message): `%s` (%s)\n",
						testDefinition.Input, testDefinition.Target))

					return nil
				}

				p.verbose(fmt.Sprintf("Test recovered (min duration cache cleared, showing recovered message): `%s` (%s)\n",
					testDefinition.Input, testDefinition.Target))

			} else {

				/*
					If we're here, a recovered message got generated for an error that has never been seen.
					Skip it, as it is probably a broken test.
				*/

				p.verbose(fmt.Sprintf("Test recovered (min duration not met, original error never seen): `%s` (%s)\n",
					testDefinition.Input, testDefinition.Target))
				return nil

			}

		}

	}

	// If test has a deduplication rule, avoid re-triggering a notification if not needed, or clean the dedup cache if needed.
	if testDefinition.DedupDuration != nil {

		hash := testResult.Hash()
		if testResult.Error != nil {

			// Save the current notification time, this keeps alive the deduplication. *10 so that it's not going to expire
			// anytime soon.
			p.setDeduplicationCacheTime(hash, *testDefinition.DedupDuration*10)

			lastAlertTime := p.getDeduplicationLastAlertTime(hash)

			// With dedup, we don't want to trigger same notification, unless we just passed the dedup duration
			if lastAlertTime != nil {
				diffLastAlert := now.Unix() - *lastAlertTime
				dedupDurationSeconds := int64(*testDefinition.DedupDuration / time.Second)

				if diffLastAlert < dedupDurationSeconds {
					// There is no need to trigger the notification, because not enough time has passed since the last one
					p.verbose(fmt.Sprintf("Skipping notification (dedup, last notif %s ago) for test `%s` (%s)\n",
						time.Duration(diffLastAlert)*time.Second,
						testDefinition.Input, testDefinition.Target))
					return nil
				}

				// Let the user know that the generated notification is a duplicate
				testResult.IsDedup = true
			}

			p.setDeduplicationLastAlertTime(hash, *testDefinition.DedupDuration*10)

		} else {
			// Check if a dedup was happening
			dedupCacheTime := p.getDeduplicationCacheTime(hash)

			// If there was a dedup cache time, we can mark this test as recovered
			if dedupCacheTime != nil {
				// Clear any dedup cache, because the test has passed
				p.clearDeduplicationCacheTime(hash)
				p.clearDeduplicationLastAlertTime(hash)
				testResult.Recovered = true

				p.verbose(fmt.Sprintf("Test recovered (dedup cache cleared): `%s` (%s)\n",
					testDefinition.Input, testDefinition.Target))
			}

		}

	}

	//
	// Convert the test result to a JSON string we can notify.
	//
	j, err := json.Marshal(testResult)
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

func (p *workerCmd) getDeduplicationCacheKey(hash string) string {
	return fmt.Sprintf("overseer.dedup-cache.%s", hash)
}

func (p *workerCmd) getDeduplicationCacheTime(hash string) *int64 {
	if p._r == nil {
		return nil
	}

	cacheKey := p.getDeduplicationCacheKey(hash)
	cacheTime, err := p._r.Get(cacheKey).Int64()
	if err != nil {
		if err == redis.Nil {
			// Key just does not exist
			return nil
		}

		fmt.Printf("Failed to get dedup cache key: %s\n", err)
		return nil
	}

	return &cacheTime
}

func (p *workerCmd) setDeduplicationCacheTime(hash string, expiry time.Duration) {
	if p._r == nil {
		return
	}

	cacheKey := p.getDeduplicationCacheKey(hash)
	_, err := p._r.Set(cacheKey, time.Now().Unix(), expiry).Result()
	if err != nil {
		fmt.Printf("Failed to set dedup cache key: %s\n", err)
	}
}

func (p *workerCmd) clearDeduplicationCacheTime(hash string) {
	if p._r == nil {
		return
	}

	cacheKey := p.getDeduplicationCacheKey(hash)
	_, err := p._r.Del(cacheKey).Result()
	if err != nil {
		fmt.Printf("Failed to clear dedup cache key: %s\n", err)
	}
}

func (p *workerCmd) getDeduplicationLastAlertKey(hash string) string {
	return fmt.Sprintf("overseer.dedup-last-alert.%s", hash)
}

func (p *workerCmd) getDeduplicationLastAlertTime(hash string) *int64 {
	if p._r == nil {
		return nil
	}

	cacheKey := p.getDeduplicationLastAlertKey(hash)
	cacheTime, err := p._r.Get(cacheKey).Int64()
	if err != nil {
		if err == redis.Nil {
			// Key just does not exist
			return nil
		}

		fmt.Printf("Failed to get dedup last alert key: %s\n", err)
		return nil
	}

	return &cacheTime
}

func (p *workerCmd) setDeduplicationLastAlertTime(hash string, expiry time.Duration) {
	if p._r == nil {
		return
	}

	cacheKey := p.getDeduplicationLastAlertKey(hash)
	_, err := p._r.Set(cacheKey, time.Now().Unix(), expiry).Result()
	if err != nil {
		fmt.Printf("Failed to set dedup last alert key: %s\n", err)
	}
}

func (p *workerCmd) clearDeduplicationLastAlertTime(hash string) {
	if p._r == nil {
		return
	}

	cacheKey := p.getDeduplicationLastAlertKey(hash)
	_, err := p._r.Del(cacheKey).Result()
	if err != nil {
		fmt.Printf("Failed to clear dedup last alert key: %s\n", err)
	}
}

func (p *workerCmd) getMinDurationFirstErrorKey(hash string) string {
	return fmt.Sprintf("overseer.min-duration-first-error.%s", hash)
}

func (p *workerCmd) getMinDurationFirstErrorTime(hash string) *int64 {
	if p._r == nil {
		return nil
	}

	cacheKey := p.getMinDurationFirstErrorKey(hash)
	cacheTime, err := p._r.Get(cacheKey).Int64()
	if err != nil {
		if err == redis.Nil {
			// Key just does not exist
			return nil
		}

		fmt.Printf("Failed to get min-duration alert shown key: %s\n", err)
		return nil
	}

	return &cacheTime
}

func (p *workerCmd) setMinDurationFirstErrorTime(hash string, errorTime int64, expiry time.Duration) {
	if p._r == nil {
		return
	}

	cacheKey := p.getMinDurationFirstErrorKey(hash)
	_, err := p._r.Set(cacheKey, errorTime, expiry).Result()
	if err != nil {
		fmt.Printf("Failed to set min-duration alert shown key: %s\n", err)
	}
}

func (p *workerCmd) clearMinDurationFirstErrorTime(hash string) {
	if p._r == nil {
		return
	}

	cacheKey := p.getMinDurationFirstErrorKey(hash)
	_, err := p._r.Del(cacheKey).Result()
	if err != nil {
		fmt.Printf("Failed to clear min-duration alert shown key: %s\n", err)
	}
}

func (p *workerCmd) getMinDurationAlertShownKey(hash string) string {
	return fmt.Sprintf("overseer.min-duration-alert-shown.%s", hash)
}

func (p *workerCmd) getMinDurationAlertShown(hash string) bool {
	if p._r == nil {
		return false
	}

	cacheKey := p.getMinDurationAlertShownKey(hash)
	alertShown, err := p._r.Get(cacheKey).Int64()
	if err != nil {
		if err == redis.Nil {
			// Key just does not exist
			return false
		}

		fmt.Printf("Failed to get min-duration alert shown key: %s\n", err)
		return false
	}

	return alertShown > 0
}

func (p *workerCmd) setMinDurationAlertShown(hash string, shown bool, expiry time.Duration) {
	if p._r == nil {
		return
	}

	alertShown := 0
	if shown {
		alertShown = 1
	}

	cacheKey := p.getMinDurationAlertShownKey(hash)
	_, err := p._r.Set(cacheKey, alertShown, expiry).Result()
	if err != nil {
		fmt.Printf("Failed to set min-duration alert shown key: %s\n", err)
	}
}

func (p *workerCmd) clearMinDurationAlertShown(hash string) {
	if p._r == nil {
		return
	}

	cacheKey := p.getMinDurationAlertShownKey(hash)
	_, err := p._r.Del(cacheKey).Result()
	if err != nil {
		fmt.Printf("Failed to clear min-duration alert shown key: %s\n", err)
	}
}

// alphaNumeric removes all non alpha-numeric characters from the
// given string, and returns it.  We replace the characters that
// are invalid with `_`.
func (p *workerCmd) alphaNumeric(input string) string {
	//
	// Remove non alphanumeric
	//
	reg, err := regexp.Compile("[^A-Za-z0-9]+")
	if err != nil {
		panic(err)
	}
	return reg.ReplaceAllString(input, "_")
}

// formatMetrics Format a test for metrics submission.
//
// This is a little weird because ideally we'd want to submit to the
// metrics-host :
//
//    overseer.$testType.$testTarget.$key => value
//
// But of course the target might not be what we think it is for all
// cases - i.e. A DNS test the target is the name of the nameserver rather
// than the thing to lookup, which is the natural target.
//
func (p *workerCmd) formatMetrics(tst test.Test, key string) string {

	prefix := "overseer.test."

	//
	// Special-case for the DNS-test
	//
	if tst.Type == "dns" {
		return prefix + ".dns." + p.alphaNumeric(tst.Arguments["lookup"]) + "." + key
	}

	//
	// Otherwise we have a normal test.
	//
	return prefix + tst.Type + "." + p.alphaNumeric(tst.Target) + "." + key
}

// runTest is really the core of our application, as it is responsible
// for receiving a test to execute, executing it, and then issuing
// the notification with the result.
func (p *workerCmd) runTest(workerIdx uint, tst test.Test, opts test.Options) error {

	workerPrefix := fmt.Sprintf("[W%d] ", workerIdx)

	// Create a map for metric-recording.
	metricsLock := new(sync.Mutex)
	metrics := map[string]string{}

	// If there are no deduplication rules, assign the default worker one. Unless the test is a period-test
	if tst.DedupDuration == nil && tst.PeriodTestDuration == nil && p.DedupDuration > 0 {
		// Assign a default dedup duration
		tst.DedupDuration = &p.DedupDuration
	}

	// If there are no min duration rules, assign the default worker one. Unless the test is a period-test
	if tst.MinDuration == nil && tst.PeriodTestDuration == nil && p.MinDuration > 0 {
		// Assign a default min duration
		tst.MinDuration = &p.MinDuration
	}

	// If there are no min duration cache rules, assign the default worker one. Unless the test is a period-test
	if tst.MinDurationCacheFactor == 0 && tst.PeriodTestDuration == nil && p.MinDurationCacheFactor > 0 {
		// Assign a default min duration
		tst.MinDurationCacheFactor = p.MinDurationCacheFactor
	}

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

	// If we're dealing with hostname-based testing, then resolve hostnames
	if tmp.ShouldResolveHostname() {

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

		// Record the time before we lookup our targets IPs.
		timeA := time.Now()

		// Now resolve the target to IPv4 & IPv6 addresses.
		ips, err := net.LookupIP(testTarget)
		if err != nil {

			//
			// We failed to resolve the target, so we have to raise
			// a failure.  But before we do that we need to sanitize
			// the test.
			//
			tst.Input = tst.Sanitize()

			//
			// Notify the world about our DNS-failure.
			//
			p.notify(tst, nil, fmt.Errorf("failed to resolve name %s", testTarget), nil)

			//
			// Otherwise we're done.
			//
			fmt.Printf(workerPrefix+"WARNING: Failed to resolve %s for %s test!\n", testTarget, testType)
			return err
		}

		// Calculate the time the DNS-resolution took - in milliseconds.
		timeB := time.Now()
		duration := timeB.Sub(timeA)
		diff := fmt.Sprintf("%f", float64(duration)/float64(time.Millisecond))

		// Record time in our metric hash
		metricsLock.Lock()
		metrics["overseer.dns."+p.alphaNumeric(testTarget)+".duration"] = diff
		metricsLock.Unlock()

		//
		// We'll run the test against each of the resulting IPv4 and
		// IPv6 addresess - ignoring any IP-protocol which is disabled.
		//
		// Save the results in our `targets` array, unless disabled.
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

	} else {
		// Directly pass the original target
		targets = append(targets, testTarget)
	}

	if tst.MaxTargetsCount > 0 && len(targets) > tst.MaxTargetsCount {
		// By sorting we have a higher chance of targeting the same target
		sort.Strings(targets)
		targets = targets[:tst.MaxTargetsCount]
	}

	testEndFn := func(startTime time.Time, target string, attempts uint, result error, details *string) {
		//
		// Now the test is complete we can record the time it
		// took to carry out, and the number of attempts it
		// took to complete.
		//
		timeB := time.Now()
		duration := timeB.Sub(startTime)
		diff := fmt.Sprintf("%f", float64(duration)/float64(time.Millisecond))
		metricsLock.Lock()
		metrics[p.formatMetrics(tst, "duration")] = diff
		metrics[p.formatMetrics(tst, "attempts")] = fmt.Sprintf("%d", attempts)
		metricsLock.Unlock()

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
		tstCopy := tst
		tstCopy.Target = target

		//
		// We also want to filter out any password which was found
		// on the input-line.
		//
		tstCopy.Input = tst.Sanitize()

		//
		// Now we can trigger the notification with our updated
		// copy of the test.
		//
		p.notify(tstCopy, tmp.GetUniqueHashForTest(tstCopy, opts), result, details)
	}

	wg := &sync.WaitGroup{}

	//
	// Now for each target, run the test.
	//
	for _, target := range targets {
		wg.Add(1)
		go func() {

			// Is this a period test?
			if tst.PeriodTestDuration != nil {
				periodTestDuration := *tst.PeriodTestDuration
				periodTestSleep := tst.PeriodTestSleep
				if periodTestSleep == 0 {
					periodTestSleep = p.PeriodTestSleep
				}
				periodTestThreshold := p.PeriodTestThreshold
				if tst.PeriodTestThreshold != nil {
					periodTestThreshold = *tst.PeriodTestThreshold
				}

				p.verbose(fmt.Sprintf(workerPrefix+"Running '%s' period-test (duration: %s, sleep: %s, threshold: %.0f%%) against %s (%s)\n", testType, periodTestDuration, periodTestSleep, periodTestThreshold, testTarget, target))

				// Start time
				timeStart := time.Now()
				timeEnd := timeStart.Add(periodTestDuration)

				var countSuccess uint = 0
				var countFail uint = 0

				iteration := 0
				var errorStrings []string
				for time.Now().Before(timeEnd) {
					iteration++
					iterationStartTime := time.Now()

					// Copy opts
					currentOpts := opts
					currentOpts.PeriodTestIndex = iteration
					currentOpts.PeriodTestStartTime = iterationStartTime.UnixNano() / int64(time.Millisecond)
					err := tmp.RunTest(tst, target, currentOpts)

					iterationDuration := time.Since(iterationStartTime)
					iterationElapsedString := fmt.Sprintf("%.2fms", float64(iterationDuration)/float64(time.Millisecond))
					if err != nil {
						countFail++
						p.verbose(fmt.Sprintf(workerPrefix+"Period-test (test %d failed, took %s): %s\n", iteration, iterationElapsedString, err.Error()))
						errString := fmt.Sprintf("test %d failed, took %s: %s", iteration, iterationElapsedString, err.Error())
						errorStrings = append(errorStrings, errString)
					} else {
						countSuccess++
						p.verbose(fmt.Sprintf(workerPrefix+"Period-test (test %d success, took %s)\n", iteration, iterationElapsedString))
					}

					time.Sleep(periodTestSleep)
				}

				totalAttempts := countFail + countSuccess

				errPercentage := float32(countFail) / float32(totalAttempts)
				var result error
				var failuresString *string
				if len(errorStrings) > 0 {
					var lines []string
					for _, errorString := range errorStrings {
						lines = append(lines, fmt.Sprintf("- %s", errorString))
					}
					output := fmt.Sprintf("Period-test errors:\n%s", strings.Join(lines, "\n"))
					failuresString = &output
				}
				if errPercentage > periodTestThreshold {
					result = fmt.Errorf("%d tests failed out of %d (%.2f%%)", countFail, totalAttempts, errPercentage*100)
					p.verbose(fmt.Sprintf(workerPrefix+"Test failed: %s\n", result.Error()))
				} else {
					p.verbose(fmt.Sprintf(workerPrefix+"Test passed: %d tests failed out of %d (%.2f%%)\n", countFail, totalAttempts, errPercentage*100))
				}

				testEndFn(timeStart, target, totalAttempts, result, failuresString)
				wg.Done()
				return
			}

			p.verbose(fmt.Sprintf(workerPrefix+"Running '%s' test against %s (%s)\n", testType, testTarget, target))

			//
			// We'll repeat failing tests up to five times by default
			//
			var attempt uint = 0
			var maxAttempts uint = p.RetryCount

			//
			// If retrying is disabled then don't retry.
			//
			if !p.Retry {
				maxAttempts = attempt + 1
			}

			if tst.MaxRetries != nil {
				maxAttempts = *tst.MaxRetries + 1
			}

			//
			// The result of the test.
			//
			var result error

			//
			// Record the start-time of the test.
			//
			timeA := time.Now()

			//
			// Start the count here for graphing execution attempts.
			//
			var c uint = 0

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
				attempt++
				c++

				//
				// Run the test
				//
				result = tmp.RunTest(tst, target, opts)

				//
				// If the test passed then we're good.
				//
				if result == nil {
					p.verbose(fmt.Sprintf(workerPrefix+"[%d/%d] - Test passed.\n", attempt, maxAttempts))

					// break out of loop
					attempt = maxAttempts + 1

				} else {

					//
					// The test failed.
					//
					// It will be repeated before a notifier
					// is invoked.
					//
					p.verbose(fmt.Sprintf(workerPrefix+"[%d/%d] Test failed: %s\n", attempt, maxAttempts, result.Error()))

					// If there are no more retries, do not wait
					if maxAttempts-attempt > 0 {
						//
						// Sleep before retrying the failing test.
						//
						p.verbose(fmt.Sprintf(workerPrefix+"Sleeping for %s before retrying\n", p.RetryDelay.String()))

						time.Sleep(p.RetryDelay)
					}
				}
			}

			testEndFn(timeA, target, c, result, nil)
			wg.Done()
		}()
	}

	wg.Wait()

	//
	// If we have a metric-host we can now submit each of the values
	// to it.
	//
	// There will be three results for each test:
	//
	//  1.  The DNS-lookup-time of the target.
	//
	//  2.  The time taken to run the test.
	//
	//  3.  The number of attempts (retries, really) before the
	//      test was completed.
	//
	if p._g != nil {
		for key, val := range metrics {
			v := os.Getenv("METRICS_VERBOSE")
			if v != "" {
				fmt.Printf("%s %s\n", key, val)
			}

			p._g.SimpleSend(key, val)
		}
	}

	return nil
}

//
// Entry-point.
//
func (p *workerCmd) Execute(_ context.Context, f *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {

	// Sanity check
	if p.Parallel == 0 {
		fmt.Printf("Number of parallel workers must be > 0")
		return subcommands.ExitFailure
	}

	//
	// Connect to the redis-host.
	//
	if p.RedisSocket != "" {
		p._r = redis.NewClient(&redis.Options{
			Network:     "unix",
			Addr:        p.RedisSocket,
			Password:    p.RedisPassword,
			DB:          p.RedisDB,
			DialTimeout: p.RedisDialTimeout,
		})
	} else {
		p._r = redis.NewClient(&redis.Options{
			Addr:        p.RedisHost,
			Password:    p.RedisPassword,
			DB:          p.RedisDB,
			DialTimeout: p.RedisDialTimeout,
		})
	}

	//
	// And run a ping, just to make sure it worked.
	//
	_, err := p._r.Ping().Result()
	if err != nil {
		fmt.Printf("Redis connection failed: %s\n", err.Error())
		return subcommands.ExitFailure
	}

	//
	// Setup our metrics-connection, if enabled
	//
	p.MetricsFromEnvironment()

	//
	// Setup the options passed to each test, by copying our
	// global ones.
	//
	var opts test.Options
	opts.Verbose = p.Verbose
	opts.Timeout = p.Timeout

	//
	// Create a parser for our input
	//
	parse := parser.New()

	// We want a graceful shutdown, e.g. if a long-running test is active at the moment we need to wait for it to
	// complete before brutally exiting!
	shouldExit := sync.NewCond(&sync.Mutex{})
	onSignalInterrupt(func() {
		shouldExit.Broadcast()

		// If there is a second interrupt, immediately exit
		onSignalInterrupt(func() {
			os.Exit(0)
		})
	})

	wg := &sync.WaitGroup{}
	var idx uint
	for idx = 1; idx <= p.Parallel; idx++ {
		workerIdx := idx
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.workerLoop(workerIdx, shouldExit, &opts, parse)
		}()
	}

	wg.Wait()

	return subcommands.ExitSuccess
}

func (p *workerCmd) workerLoop(workerIdx uint, shouldExit *sync.Cond, opts *test.Options, parse *parser.Parser) {
	fmt.Printf("worker %d started [tag=%s]\n", workerIdx, p.Tag)

	exitLock := &sync.Mutex{}
	exit := false

	workerAvailableChan := make(chan bool)
	testObjectChan := make(chan []string)

	go func() {
		shouldExit.L.Lock()
		defer shouldExit.L.Unlock()
		shouldExit.Wait()

		exitLock.Lock()
		defer exitLock.Unlock()
		exit = true
		close(testObjectChan)
		close(workerAvailableChan)
	}()

	go func() {
		for <-workerAvailableChan {
			exitLock.Lock()
			if exit {
				exitLock.Unlock()
				return
			}
			exitLock.Unlock()

			// Get a job.
			testObject, _ := p._r.BLPop(0, "overseer.jobs").Result()

			exitLock.Lock()
			if exit {
				exitLock.Unlock()
				if len(testObject) >= 1 {
					// Requeue! Let's not lose the test
					if _, err := p._r.RPush("overseer.jobs", testObject[1]).Result(); err != nil {
						fmt.Printf("failed to requeue job `%s`: %v\n", testObject[1], err)
					} else {
						fmt.Printf("job requeued: %s\n", testObject[1])
					}
				} else {
					fmt.Printf("Popped unsupported value: %v\n", testObject)
				}
				return
			}
			exitLock.Unlock()
			testObjectChan <- testObject
		}
	}()

	// Wait for jobs
	workerAvailableChan <- true
	for testObject := range testObjectChan {
		//
		// Parse it
		//
		//   testObject[0] will be "overseer.jobs"
		//
		//   testObject[1] will be the value removed from the list.
		//
		if len(testObject) >= 1 {
			var job test.Test
			job, err := parse.ParseLine(testObject[1], nil)

			if err == nil {
				p.runTest(workerIdx, job, *opts)
			} else {
				fmt.Printf("Error parsing job from queue: %s - %s\n", testObject[1], err.Error())
			}
		} else {
			fmt.Printf("Popped unsupported value: %v\n", testObject)
		}

		exitLock.Lock()
		if exit {
			exitLock.Unlock()
			break
		}
		exitLock.Unlock()
		workerAvailableChan <- true
	}

	fmt.Printf("Worker %d exiting\n", workerIdx)
}
