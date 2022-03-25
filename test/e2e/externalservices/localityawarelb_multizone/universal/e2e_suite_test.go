package universal_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"

	"github.com/kumahq/kuma/pkg/test"
	"github.com/kumahq/kuma/test/e2e/externalservices/localityawarelb_multizone/universal"
)

func TestE2E(t *testing.T) {
	test.RunSpecs(t, "E2E External Services Locality Universal Suite")
}

var _ = Describe("Test ExternalServices on Multizone Universal with LocalityAwareLb", universal.ExternalServicesOnMultizoneUniversalWithLocalityAwareLb)
