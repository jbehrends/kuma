package egress

import (
	"sort"

	mesh_proto "github.com/kumahq/kuma/api/mesh/v1alpha1"
	core_xds "github.com/kumahq/kuma/pkg/core/xds"
	envoy_common "github.com/kumahq/kuma/pkg/xds/envoy"
	envoy_clusters "github.com/kumahq/kuma/pkg/xds/envoy/clusters"
	envoy_endpoints "github.com/kumahq/kuma/pkg/xds/envoy/endpoints"
	envoy_listeners "github.com/kumahq/kuma/pkg/xds/envoy/listeners"
	envoy_names "github.com/kumahq/kuma/pkg/xds/envoy/names"
	"github.com/kumahq/kuma/pkg/xds/envoy/tls"
)

type ZoneExternalServicesGenerator struct {
}

// Generate will generate envoy resources for one mesh (when mTLS enabled)
func (g *ZoneExternalServicesGenerator) Generate(
	proxy *core_xds.Proxy,
	listenerBuilder *envoy_listeners.ListenerBuilder,
	meshResources *core_xds.MeshResources,
) (*core_xds.ResourceSet, error) {
	resources := core_xds.NewResourceSet()

	apiVersion := proxy.APIVersion
	endpointMap := meshResources.EndpointMap
	destinations := buildDestinations(meshResources.TrafficRoutes)
	services := g.buildServices(endpointMap, proxy.ZoneEgressProxy.ZoneEgressResource.Spec.GetZone())
	meshName := meshResources.Mesh.GetMeta().GetName()

	g.addFilterChains(
		apiVersion,
		destinations,
		endpointMap,
		proxy,
		listenerBuilder,
		meshResources,
	)

	cds, err := g.generateCDS(meshName, apiVersion, services, destinations)
	if err != nil {
		return nil, err
	}
	resources.Add(cds...)

	eds, err := g.generateEDS(meshName, apiVersion, services, endpointMap)
	if err != nil {
		return nil, err
	}
	resources.Add(eds...)

	return resources, nil
}

func (*ZoneExternalServicesGenerator) generateEDS(
	meshName string,
	apiVersion envoy_common.APIVersion,
	services []string,
	endpointMap core_xds.EndpointMap,
) ([]*core_xds.Resource, error) {
	var resources []*core_xds.Resource

	for _, serviceName := range services {
		endpoints := endpointMap[serviceName]
		// There is a case where multiple meshes contain services with
		// the same names, so we cannot use just "serviceName" as a cluster
		// name as we would overwrite some clusters with the latest one
		clusterName := envoy_names.GetMeshClusterName(meshName, serviceName)

		cla, err := envoy_endpoints.CreateClusterLoadAssignment(clusterName, endpoints, apiVersion)
		if err != nil {
			return nil, err
		}

		resources = append(resources, &core_xds.Resource{
			Name:     clusterName,
			Origin:   OriginEgress,
			Resource: cla,
		})
	}

	return resources, nil
}

func (*ZoneExternalServicesGenerator) generateCDS(
	meshName string,
	apiVersion envoy_common.APIVersion,
	services []string,
	destinationsPerService map[string][]envoy_common.Tags,
) ([]*core_xds.Resource, error) {
	var resources []*core_xds.Resource

	for _, serviceName := range services {
		tagSlice := envoy_common.TagsSlice(append(destinationsPerService[serviceName], destinationsPerService[mesh_proto.MatchAllTag]...))

		tagKeySlice := tagSlice.ToTagKeysSlice().Transform(envoy_common.Without(mesh_proto.ServiceTag), envoy_common.With("mesh"))

		// There is a case where multiple meshes contain services with
		// the same names, so we cannot use just "serviceName" as a cluster
		// name as we would overwrite some clusters with the latest one
		clusterName := envoy_names.GetMeshClusterName(meshName, serviceName)

		edsCluster, err := envoy_clusters.NewClusterBuilder(apiVersion).
			Configure(envoy_clusters.EdsCluster(clusterName)).
			Configure(envoy_clusters.LbSubset(tagKeySlice)).
			Configure(envoy_clusters.DefaultTimeout()).
			Build()

		if err != nil {
			return nil, err
		}

		resources = append(resources, &core_xds.Resource{
			Name:     clusterName,
			Origin:   OriginEgress,
			Resource: edsCluster,
		})
	}

	return resources, nil
}

func (ZoneExternalServicesGenerator) buildServices(
	endpointMap core_xds.EndpointMap,
	zone string,
) []string {
	var services []string

	for serviceName, endpoints := range endpointMap {
		if len(endpoints) > 0 &&
			isZoneExternalService(&endpoints[0]) &&
			isNotSpecificZoneExternalService(&endpoints[0], zone) {
			services = append(services, serviceName)
		}
	}

	sort.Strings(services)

	return services
}

func (*ZoneExternalServicesGenerator) addFilterChains(
	apiVersion envoy_common.APIVersion,
	destinationsPerService map[string][]envoy_common.Tags,
	endpointMap core_xds.EndpointMap,
	proxy *core_xds.Proxy,
	listenerBuilder *envoy_listeners.ListenerBuilder,
	meshResources *core_xds.MeshResources,
) {
	meshName := meshResources.Mesh.GetMeta().GetName()

	sniUsed := map[string]bool{}

	for _, zoneIngress := range proxy.ZoneEgressProxy.ZoneIngresses {
		for _, service := range zoneIngress.Spec.GetAvailableServices() {
			serviceName := service.Tags[mesh_proto.ServiceTag]
			if service.Mesh != meshName {
				continue
			}

			endpoints := endpointMap[serviceName]

			if len(endpoints) == 0 {
				// There is no need to generate filter chain if there is no
				// endpoints for the service
				continue
			}

			if isNotExternalService(&endpoints[0]) {
				// We need to generate filter chain for external services only
				continue
			}

			if isZoneExternalService(&endpoints[0]) && isSpecificZoneExternalService(&endpoints[0], zoneIngress.Spec.Zone) {

				destinations := destinationsPerService[serviceName]
				destinations = append(destinations, destinationsPerService[mesh_proto.MatchAllTag]...)

				for _, destination := range destinations {
					meshDestination := destination.
						WithTags(mesh_proto.ServiceTag, serviceName).
						WithTags("mesh", meshName)

					sni := tls.SNIFromTags(meshDestination)

					if sniUsed[sni] {
						continue
					}

					sniUsed[sni] = true

					// There is a case where multiple meshes contain services with
					// the same names, so we cannot use just "serviceName" as a cluster
					// name as we would overwrite some clusters with the latest one
					clusterName := envoy_names.GetMeshClusterName(meshName, serviceName)

					listenerBuilder.Configure(envoy_listeners.FilterChain(
						envoy_listeners.NewFilterChainBuilder(apiVersion).Configure(
							envoy_listeners.MatchTransportProtocol("tls"),
							envoy_listeners.MatchServerNames(sni),
							envoy_listeners.TcpProxyWithMetadata(clusterName, envoy_common.NewCluster(
								envoy_common.WithName(clusterName),
								envoy_common.WithService(serviceName),
								envoy_common.WithTags(meshDestination.WithoutTags(mesh_proto.ServiceTag)),
							)),
						),
					))
				}
			}
		}
	}
}
