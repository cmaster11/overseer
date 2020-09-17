# Changelog

## [2020/05/30] cmaster11/overseer:1.13.3

* Overseer now supports some new test options:
    * `min-duration`: when defined, an alert will NOT be triggered until the test error is repeated for AT LEAST `min-duration` time.
        
        Example: `https://google.com must run http with min-duration 1m` will not generate alerts until the same test fails for more than a minute.
        
    * `test-label`: when defined, offers more flexibility to test result messages, where the field `testLabel` is now passed as it is. 
        This option is useful to have "clean" test result messages, where the test label can be shown instead of the original test input.
    
        Example: `https://google.com must run http with test-label "Google test"`

## [2020/05/11] cmaster11/overseer:1.12.12

* Overseer now supports two new runtime period-test variables for HTTP test:

    * `__pt-index__`: current period test index
    * `__pt-time-ms__`: current period test start time
    
    Example: `https://google.com?testIdx=__pt-index__ must run http with connect-timeout 2.5s with connect-retries 2 with pt-duration 1m with pt-sleep 1s with pt-threshold 0%`

## [2020/05/11] cmaster11/overseer:1.12.11

* Sort targets alphabetically when using `max-targets`, which increases the chance of pointing to the same target
    on subsequent runs.

## [2020/05/06] cmaster11/overseer:1.12.10

* HTTP test has new option:
    * `follow-redirect true|(\d+)`: allows following HTTP redirects.
        * `follow-redirect true`: allows max 10 redirects
        * `follow-redirect 25`: allows max 25 (or any other used number) redirects
* All tests which resolve hostnames have a new option:
    * `max-targets 2`: forces the test to run against only the first 2 (or any other number) found targets of the hostname, instead of all of them.

## [2019/12/17] cmaster11/overseer:1.12.8

* BREAKING: new behavior for `k8s-event-watcher` rules. Events will now be marked as errors **only** if `errorRules` rules exist and are matched:
```yaml
filters:
- rules:
    involvedObject.kind: Job
    involvedObject.name: "^*.fail"
    reason: BackoffLimitExceeded
  errorRules:
    # Any matched event is an error
    type: .*
```

## [2019/12/17] cmaster11/overseer:1.12.7

* BREAKING: new version of [k8s-event-watcher](https://github.com/cmaster11/k8s-event-watcher):
    * Filters configuration now accepts any field name, and a new `rules` keyword has been introduced:
    ```yaml
    filters:
    - rules:
        involvedObject.kind: Job
        involvedObject.name: "^*.fail"
        reason: BackoffLimitExceeded
    ```
* You can now use the `connect-retries` flag for HTTP tests, which will have Overseer retry the connection to a server in case of connect timeout.

## [2019/12/16] cmaster11/overseer:1.12.5

* You can now use a per-test `timeout` flag, which will override the worker one if set.
* HTTP test has new timeouts:
    * `connect-timeout`: fails if worker takes too long to establish a connection to the server.
    * `tls-timeout`: fails if worker takes too long to perform the TLS handshake with the server.
    * `resp-header-timeout`: fails if server takes too long to send headers back.

## [2019/12/11] cmaster11/overseer:1.12.4

* More reasonable release tag names.
* You can now perform [period-tests](./README.md#period-tests):

> What if you want to test how many times your web service fails in 1 minute?

## [2019/12/10] cmaster11/overseer:release-1.12

* Overseer runs now by default multiple tests in parallel (defaults to num of CPUs). This behavior is tunable with the cli flag `-parallel`:

```
overseer worker -parallel 9
```

## [2019/10/31] cmaster11/overseer:release-1.11-5

* Added support to accept multiple status codes when performing HTTP checks:

```
https://www.google.com must run http with status 200,301
```

## [2019/10/26] cmaster11/overseer:release-1.11-4

* Added support for queue-bridge filters. Destination queues (`-dest-queue=overseer.results.email`) can now be filtered
by various test result tags: `-dest-queue=overseer.results.cronjobs[type=k8s-event,target=my-namespace/Job]`. More in 
the [Kubernetes example](example-kubernetes/overseer-bridge-queue.optional.yaml) and 
[source code](bridges/queue-bridge/filter.go).