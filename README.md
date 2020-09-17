[![Go Report Card](https://goreportcard.com/badge/github.com/cmaster11/overseer)](https://goreportcard.com/report/github.com/cmaster11/overseer)
[![license](https://img.shields.io/github/license/cmaster11/overseer.svg)](https://github.com/cmaster11/overseer/blob/master/LICENSE)

Changelog is [here](CHANGELOG.md).

# DISCLAIMER

This project is a heavily modified version of the amazing [skx/overseer](https://github.com/skx/overseer) one. 
Compatibility between the two projects's data is not guaranteed, and should not be expected.

Table of Contents
=================

* [Overseer](#overseer)
* [Installation](#installation)
  * [Kubernetes](#kubernetes)
  * [Dependencies](#dependencies)
* [Executing Tests](#executing-tests)
  * [Parallel execution](#parallel-execution)
  * [Period-tests](#period-tests)
  * [Local testing](#local-testing)
  * [Running Automatically](#running-automatically)
  * [Smoothing Test Failures](#smoothing-test-failures)
* [Notifications](#notifications)
  * [Deduplication](#deduplication)
* [Metrics](#metrics)
* [Redis Specifics](#redis-specifics)

# Overseer

Overseer is a simple and scalable [golang](https://golang.org/)-based remote protocol tester, which allows you to monitor the state of your network, and the services running upon it.

"Remote Protocol Tester" sounds a little vague, so to be more concrete this application lets you test that (remote) services are running, and has built-in support for performing testing against:

* DNS-servers
   * Test lookups of A, AAAA, MX, NS, and TXT records.
* Finger
* FTP
* HTTP & HTTPS fetches.
   * HTTP basic-authentication is supported.
   * Requests may be DELETE, GET, HEAD, POST, PATCH, POST, & etc.
   * SSL certificate validation and expiration warnings are supported.
* IMAP & IMAPS
* Kubernetes service endpoints check
* MySQL
* NNTP
* ping / ping6
* POP3 & POP3S
* Postgres
* redis
* rsync
* SMTP
* SSH
* SSL
* Telnet
* VNC
* XMPP

(The implementation of the protocol-handlers can be found beneath the top-level [protocols/](protocols/) directory in this repository.)

Tests to be executed are defined in a simple text-based format which has the general form:

     $TARGET must run $SERVICE [with $OPTION_NAME $VALUE] ..

You can see what the available tests look like in [the sample test-file](input.txt), and each of the included protocol-handlers are self-documenting which means you can view example usage via:

     ~$ overseer examples [pattern]

All protocol-tests transparently support testing IPv4 and IPv6 targets, although you may globally disable either address family if you wish.

## Installation

To install locally the project:

    git clone https://github.com/cmaster11/overseer
    cd overseer
    go install

### Kubernetes

A sample deployment is provided in the [`example-kubernetes`](./example-kubernetes/) folder. Please take a look at the 
[`README`](./example-kubernetes/README.md) for more instructions.

### Dependencies

Beyond the compile-time dependencies overseer requires a [redis](https://redis.io/) server which is used for two things:

* As the storage-queue for parsed-jobs.
* As the storage-queue for test-results.

Because overseer is executed in a distributed fashion tests are not executed
as they are parsed/read, instead they are inserted into a redis-queue. A worker,
or number of workers, poll the queue fetching & executing jobs as they become
available.

In small-scale deployments it is probably sufficient to have a single worker,
and all the software running upon a single host. For a larger number of
tests (1000+) it might make more sense to have a pool of hosts each running
a worker.

Because we don't want to be tied to a specific notification-system results
of each test are also posted to the same redis-host, which allows results to be retrieved and transmitted to your preferred notifier.

More details about [notifications](#notifications) are available later in this document.

## Executing Tests

As mentioned already executing tests a two-step process:

* First of all tests are parsed and inserted into a redis-based queue.
* Secondly the tests are pulled from that queue and executed.

This might seem a little convoluted, however it is a great design if you
have a lot of tests to be executed, because it allows you to deploy multiple
workers. Instead of having a single host executing all the tests you can
can have 10 hosts, each watching the same redis-queue pulling jobs, & executing
them as they become available.

In short using a central queue allows you to scale out the testing horizontally.

To add your tests to the queue you should run:

    $ overseer enqueue \
        -redis-host=queue.example.com:6379 [-redis-pass='secret.here'] \
        test.file.1 test.file.2 .. test.file.N

This will parse the tests contained in the specified files, adding each of them to the (shared) redis queue. 
Once all of the jobs have been parsed and inserted into the queue the process will terminate.

To drain the queue you can should now start a worker, which will fetch the tests and process them:

    $ overseer worker -verbose \
        -redis-host=queue.example.com:6379 [-redis-pass='secret']

The worker will run constantly, not terminating unless manually killed. With
the worker running you can add more jobs by re-running the `overseer enqueue`
command.

To run tests in parallel simply launch more instances of the worker, on the same host, or on different hosts.

### Parallel execution

By default the worker will process in parallel a number of tests equal to the number of the current machine's logical
CPUs. To alter this behavior, you can use the `-parallel` flag:

    $ # Runs 9 tests at a time
    $ overseer worker -parallel 9
    
Using a higher number of parallel tests is useful if running any long-running tests, to not delay executions of any others.

### Period-tests

Let's imagine that you want to test how many times your web service fails in 1 minute. You can run period-tests:

    https://example.com must run http with pt-duration 60s with pt-sleep 2s with pt-threshold 15%
    
The previous line will trigger a period-test, where the same test `https://example.com must run http` 
will be tested over and over for a duration of 60 seconds (`pt-duration 60s`), with a pause of 2 seconds (`pt-sleep 2s`)
between each test. At the end of the testing period, if the percentage of errors is higher than 15% (`pt-threshold 15%`), 
an alert will be generated, e.g:

    8 tests failed out of 21 (38.10%)
    
You can also test multiple cases with a dumb test:

    dumb-test1 must run dumb-test with pt-duration 5s with pt-sleep 200ms with pt-threshold 0% with dumb-duration-max 100ms
    dumb-test2 must run dumb-test with pt-duration 5s with pt-sleep 200ms with pt-threshold 20% with dumb-duration-max 100ms
    dumb-test3 must run dumb-test with pt-duration 5s with pt-sleep 200ms with pt-threshold 40% with dumb-duration-max 100ms
    
If no `pt-sleep` is defined, Overseer will default to the `-period-test-sleep` command line variable value, or to `5s`.
If no `pt-threshold` is defined, Overseer will default to the `-period-test-threshold` command line variable value, or to `0%`.

Note: the `pt-` flags are shortened versions of the also usable longer tags:
 
    pt-duration -> period-test-duration
    pt-sleep -> period-test-sleep
    pt-threshold -> period-test-threshold
    
Note: period-tests, by default, have no enabled [deduplication](#deduplication) rules. To enable deduplication, you need
to manually add the `with dedup 5m` flag.
    
### Local testing

You can test Overseer functionalities locally using some scripts.

Setup Overseer with:

* Run a local redis: `./scripts/test-run-redis.sh` (hosts the processing queue)
* Run a local worker: `./scripts/test-run-worker.sh` (runs the actual tests)
* Run a local webhook bridge: `./scripts/test-run-webhook-bridge.sh` (fetches test results from the queue and triggers the webhook)
* Run a local http webhook listener: `./scripts/test-run-http-dump.sh` (listens for webhooks requests and dumps them to `stdout`)

Run tests with:

* Sample dumb tests: `./scripts/test-run-enqueue.sh`
* An always-failing test: `./scripts/test-run-enqueue-fail.sh`
* An sample period-test: `./scripts/test-run-enqueue-period.sh`
* Custom rules: `./scripts/test-run-enqueue-stdin.sh "https://google.com must run http"`

### Running Automatically

Beneath [systemd/](systemd/) you will find some sample service-files which can be used to deploy overseer upon a single host:

* A service to start a single worker, fetching jobs from a redis server.
  * The redis-server is assumed to be running on `localhost`.
* A service & timer to regularly populate the queue with fresh jobs to be executed.
  * i.e. The first service is the worker, this second one feeds the worker.

### Smoothing Test Failures

To avoid triggering false alerts due to transient (network/host) failures
tests which fail are retried several times before triggering a notification.

This _smoothing_ is designed to avoid raising an alert, which then clears
upon the next overseer run, but the downside is that flapping services might
not necessarily become visible.

If you're absolutely certain that your connectivity is good, and that
alerts should always be raised for failing services you can disable this
retry-logic via the command-line flag `-retry=false`.

## Notifications

The result of each test is submitted to the central redis-host, from where it can be pulled and used to notify a human of a problem.

Sample result-processors are [included](bridges/) in this repository which post test-results via [webhook](bridges/webhook-bridge/main.go) (e.g. to trigger notifications with [Notify17](https://notify17.net), to a [purppura instance](https://github.com/skx/purppura) or via email.

The sample bridges are primarily included for demonstration purposes, the
expectation is you'll prefer to process the results and issue notifications to
humans via your favourite in-house tool - be it [Notify17](https://notify17.net), or something similar.

The results themselves are published as JSON objects to the `overseer.results` set. Your notifier should remove the results from this set, as it generates alerts to prevent it from growing indefinitely.

You can check the size of the results set at any time via `redis-cli` like so:

    $ redis-cli llen overseer.results
    (integer) 0

The JSON object used to describe each test-result has the following fields:

| Field Name | Field Value                                                                                              |
| ---------- | -------------------------------------------------------------------------------------------------------- |
| `input`    | The input as read from the configuration-file.                                                           |
| `error`    | If the test failed this will explain why, otherwise it will be null.                                     |
| `time`     | The time the result was posted, in seconds past the epoch.                                               |
| `target`   | The target of the test, either an IPv4 address or an IPv6 one.                                           |
| `type`     | The type of test (ssh, ftp, etc).                                                                        |
| `isDedup`  | If true, the alert is a duplicate of a previously triggered one (see [deduplication](#deduplication)).   |
| `recovered`| If true, the alert has recovered from a previous error (see [deduplication](#deduplication)).            |

**NOTE**: The `input` field will be updated to mask any password options which have been submitted with the tests.

As mentioned this repository contains some demonstration "[bridges](bridges/)", which poll the results from Redis, and forward them to more useful systems:

* [`webhook-bridge/main.go`](bridges/webhook-bridge/main.go)
  * Forwards each test-result to a generic URL (e.g. to trigger notifications with [Notify17](https://notify17.net)).
  * If started with the flag `-send-test-recovered=true`, tests which recovered from failure (see [deduplication](#deduplication)) are sent.
  * If started with the flag `-send-test-success=true`, successful tests are sent.
* [`queue-bridge/main.go`](bridges/queue-bridge/main.go)
  * Clones test results to multiple `-destionation-queues`, so that the can be processed by multiple other bridges, like email and webhook ([example](example-kubernetes/README.md#multiple-destinations-eg-notify17-and-email)).
* [`email-bridge/main.go`](bridges/email-bridge/main.go)
  * This posts test-failures via email.
  * If started with the flag `-send-test-recovered=true`, tests which recovered from failure (see [deduplication](#deduplication)) are sent.
  * If started with the flag `-send-test-success=true`, successful tests are sent.
* [`sendmail-bridge/main.go`](bridges/sendmail-bridge/main.go)
  * This posts test-failures via sendemail.
  * Tests which pass are not reported.
* [`purppura-bridge/main.go`](bridges/purppura-bridge/main.go)
  * This forwards each test-result to a [purppura host](https://github.com/skx/purppura/).
  
## Deduplication

**Disclaimer**: deduplication has been fully developed only for the [webhook](bridges/webhook-bridge/main.go) and [email](bridges/email-bridge/main.go) bridges.

It is possible to enable the deduplication of alerts by using the `with dedup 5m` rule, or by starting `overseer worker` with the `-dedup=5m` flag.

What deduplication does is:

- When an alert gets triggered because of an error:
  - Calculates a unique hash for the generated alert, based on the input rule.
  - If the alert has been already generated in the past, closer than the period of time specified (e.g. `5m` for 5 minutes), a new alert will NOT be triggered.
  - If the alert has been already generated in the past, but enough time has passed (e.g. > 5 min ago), a new alert will be generated, and will carry the `isDedup` flag set to `true`.
- When a test succeeds, after having failed in the past:
  - A new alert will be generated, having `error` set to `null` and `recovered` set to `true`.

## Metrics

Overseer has partial built-in support for exporting metrics to a remote carbon-server:

* Details of the system itself.
   * Via the [go-metrics](https://github.com/skx/golang-metrics) package.
* Details of the tests executed.
   * Including the time to run tests, perform DNS lookups, and retry-counts.

To enable this support simply export the environmental variable `METRICS`
with the hostname of your remote metrics-host prior to launching the worker.

## Redis Specifics

We use Redis as a queue as it is simple to deploy, stable, and well-known.

Redis doesn't natively operate as a queue, so we replicate this via the "list"
primitives. Adding a job to a queue is performed via a "[rpush](https://redis.io/commands/rpush)" operation, and pulling a job from the queue is achieved via an "[blpop](https://redis.io/commands/blpop)" command.

We use the following lists as queues:

* `overseer.jobs`
    * For storing tests to be executed by a worker.
* `overseer.results`
    * For storing results, to be processed by a notifier.

You can examine the length of either queue via the [llen](https://redis.io/commands/llen) operation.

* To view jobs pending execution:
   * `redis-cli lrange overseer.jobs 0 -1`
   * Or to view just the count
      * `redis-cli llen overseer.jobs`
* To view test-results which have yet to be notified:
   * `redis-cli lrange overseer.results 0 -1`
   * Or to view just the count
      * `redis-cli llen overseer.results`

Alberto (all original source credits to [skx](https://github.com/skx))
--
