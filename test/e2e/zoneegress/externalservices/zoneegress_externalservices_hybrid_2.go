package externalservices

import (
	"fmt"

	"github.com/gruntwork-io/terratest/modules/k8s"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	config_core "github.com/kumahq/kuma/pkg/config/core"
	. "github.com/kumahq/kuma/test/framework"
	"github.com/kumahq/kuma/test/framework/deployments/testserver"
	"github.com/kumahq/kuma/test/framework/envoy_admin/stats"
)

func Hybrid2UniversalGlobal() {
	const defaultMesh = "default"
	const nonDefaultMesh = "non-default"
	meshMTLSOn := `
type: Mesh
name: %s
mtls:
  enabledBackend: ca-1
  backends:
  - name: ca-1
    type: builtin
networking:
  outbound:
    passthrough: %s
routing:
  localityAwareLoadBalancing: true
  zoneEgress: %s
`

	externalService1 := `
type: ExternalService
mesh: %s
name: external-service-1
tags:
  kuma.io/service: external-service-1
  kuma.io/protocol: http
networking:
  address: es-test-server.default.svc.cluster.local:80
`

	externalService2 := `
type: ExternalService
mesh: %s
name: external-service-2
tags:
  kuma.io/service: external-service-2
  kuma.io/protocol: http
  kuma.io/zone: kuma-2-zone
networking:
  address: example.com:80
`

	externalService3 := `
type: ExternalService
mesh: %s
name: httpbin
tags:
  kuma.io/service: httpbin
  kuma.io/protocol: http
  kuma.io/zone: kuma-1-zone
networking:
  address: httpbin.org:80
`

	var global Cluster
	var zone1, zone2 *K8sCluster

	BeforeEach(func() {
		k8sClusters, err := NewK8sClusters(
			[]string{Kuma1, Kuma2},
			Silent)
		Expect(err).ToNot(HaveOccurred())

		universalClusters, err := NewUniversalClusters(
			[]string{Kuma5},
			Silent)
		Expect(err).ToNot(HaveOccurred())

		// Global
		global = universalClusters.GetCluster(Kuma5)

		Expect(NewClusterSetup().
			Install(Kuma(config_core.Global)).
			Install(YamlUniversal(fmt.Sprintf(meshMTLSOn, defaultMesh, "true", "true"))).
			Install(YamlUniversal(fmt.Sprintf(meshMTLSOn, nonDefaultMesh, "true", "true"))).
			Install(YamlUniversal(fmt.Sprintf(externalService1, nonDefaultMesh))).
			Install(YamlUniversal(fmt.Sprintf(externalService2, nonDefaultMesh))).
			Install(YamlUniversal(fmt.Sprintf(externalService3, nonDefaultMesh))).
			Setup(global)).To(Succeed())

		E2EDeferCleanup(global.DismissCluster)

		globalCP := global.GetKuma()

		// K8s Cluster 1
		zone1 = k8sClusters.GetCluster(Kuma1).(*K8sCluster)
		Expect(NewClusterSetup().
			Install(Kuma(config_core.Zone,
				WithIngress(),
				WithEgress(true),
				WithGlobalAddress(globalCP.GetKDSServerAddress()),
			)).
			Install(NamespaceWithSidecarInjection(TestNamespace)).
			Install(DemoClientK8s(nonDefaultMesh)).
			Install(testserver.Install(
				testserver.WithName("es-test-server"),
				testserver.WithNamespace("default"),
				testserver.WithArgs("echo", "--instance", "es-test-server"),
			)).
			Setup(zone1)).To(Succeed())

		E2EDeferCleanup(func() {
			Expect(zone1.DeleteNamespace(TestNamespace)).To(Succeed())
			Expect(zone1.DeleteKuma()).To(Succeed())
			Expect(zone1.DismissCluster()).To(Succeed())
		})

		// K8s Cluster 2
		zone2 = k8sClusters.GetCluster(Kuma2).(*K8sCluster)
		Expect(NewClusterSetup().
			Install(Kuma(config_core.Zone,
				WithIngress(),
				WithEgress(true),
				WithGlobalAddress(globalCP.GetKDSServerAddress()),
			)).
			Install(NamespaceWithSidecarInjection(TestNamespace)).
			Install(DemoClientK8s(nonDefaultMesh)).
			Install(testserver.Install(
				testserver.WithName("es-test-server"),
				testserver.WithNamespace("default"),
				testserver.WithArgs("echo", "--instance", "es-test-server"),
			)).
			Setup(zone2)).To(Succeed())

		E2EDeferCleanup(func() {
			Expect(zone2.DeleteNamespace(TestNamespace)).To(Succeed())
			Expect(zone2.DeleteKuma()).To(Succeed())
			Expect(zone2.DismissCluster()).To(Succeed())
		})

		E2EDeferCleanup(zone2.DismissCluster)
	})

	FIt("k8s should access external service through zoneegress", func() {
		filter := fmt.Sprintf(
			"cluster.%s_%s.upstream_rq_total",
			nonDefaultMesh,
			"external-service-1",
		)

		pods, err := k8s.ListPodsE(
			zone1.GetTesting(),
			zone1.GetKubectlOptions(TestNamespace),
			metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", "demo-client"),
			},
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(pods).To(HaveLen(1))

		clientPod := pods[0]

		Eventually(func(g Gomega) {
			stat, err := zone1.GetZoneEgressEnvoyTunnel().GetStats(filter)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(stat).To(stats.BeEqualZero())
		}, "30s", "1s").Should(Succeed())

		_, stderr, err := zone1.ExecWithRetries(TestNamespace, clientPod.GetName(), "demo-client",
			"curl", "--verbose", "--max-time", "3", "--fail", "external-service-12.mesh")
		Expect(err).ToNot(HaveOccurred())
		Expect(stderr).To(ContainSubstring("HTTP/1.1 200 OK"))

		Eventually(func(g Gomega) {
			stat, err := zone1.GetZoneEgressEnvoyTunnel().GetStats(filter)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(stat).To(stats.BeGreaterThanZero())
		}, "30s", "1s").Should(Succeed())
	})
}
