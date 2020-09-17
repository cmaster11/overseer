// Kubernetes Service Tester
//
// The Kubernetes service tester checks that a k8s service has more than the specified number of endpoints (default >= 1).
//
// This test is invoked via input like so:
//
//    service-domain must run k8s-svc
//

package protocols

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cmaster11/overseer/test"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	// Import all auth methods k8s
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// K8SSvcTest is our object.
type K8SSvcTest struct {
}

// Arguments returns the names of arguments which this protocol-test
// understands, along with corresponding regular-expressions to validate
// their values.
func (s *K8SSvcTest) Arguments() map[string]string {
	known := map[string]string{
		"min-endpoints": "^[0-9]+$",
	}
	return known
}

// ShouldResolveHostname returns if this protocol requires the hostname resolution of the first test argument
func (s *K8SSvcTest) ShouldResolveHostname() bool {
	return false
}

// Example returns sample usage-instructions for self-documentation purposes.
func (s *K8SSvcTest) Example() string {
	str := `
K8SSvc Tester
-------------
 The Kubernetes service tester checks that a k8s service has 
 more than the specified number of endpoints (default >= 1).

 This test is invoked via input like so:

    namespace-name/service-name must run k8s-svc

 The number of min endpoints that need to be available can be set with:

	# Requires minimum 2 endpoints to be available for the test to succeed
	service-name must run k8s-svc with min-endpoints 2
`
	return str
}

// RunTest is the part of our API which is invoked to actually execute a
// test against the given target.
func (s *K8SSvcTest) RunTest(tst test.Test, target string, opts test.Options) error {
	var err error

	//
	// The default port to connect to.
	//
	minEndpoints := 1

	parts := strings.Split(target, "/")
	if len(parts) != 2 {
		return fmt.Errorf("not a valid namespace-name/service-name target provided: %s", target)
	}

	namespace := parts[0]
	serviceName := parts[1]

	//
	// If the user specified a different port update to use it.
	//
	if tst.Arguments["min-endpoints"] != "" {
		minEndpoints, err = strconv.Atoi(tst.Arguments["min-endpoints"])
		if err != nil {
			return err
		}
	}

	var k8sConfig *rest.Config
	kubeconfigPath := os.Getenv("KUBE_CONFIG_PATH")
	if kubeconfigPath != "" {
		k8sConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return err
		}
	} else {
		k8sConfig, err = rest.InClusterConfig()
		if err != nil {
			return err
		}
	}

	clientset, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		return err
	}

	endpoints, err := clientset.CoreV1().Endpoints(namespace).Get(serviceName, v1.GetOptions{})
	if err != nil {
		return err
	}

	// Count the number of available endpoints
	endpointsCount := 0

	for _, v := range endpoints.Subsets {
		endpointsCount += len(v.Addresses)
	}

	if endpointsCount < minEndpoints {
		return fmt.Errorf("number of available endpoints (%d) is lower than min defined (%d)", endpointsCount, minEndpoints)
	}

	return nil
}

func (s *K8SSvcTest) GetUniqueHashForTest(tst test.Test, opts test.Options) *string {
	return nil
}

//
// Register our protocol-tester.
//
func init() {
	Register("k8s-svc", func() ProtocolTest {
		return &K8SSvcTest{}
	})
}
