package ingress

import (
	mesh_proto "github.com/kumahq/kuma/api/mesh/v1alpha1"
	core_mesh "github.com/kumahq/kuma/pkg/core/resources/apis/mesh"
	core_xds "github.com/kumahq/kuma/pkg/core/xds"
	"github.com/kumahq/kuma/pkg/xds/envoy"
)

const (
	// Constants for Locality Aware load balancing
	// The Highest priority 0 shall be assigned to all locally available services
	// A priority of 1 is for ExternalServices and services exposed on neighboring ingress-es
	priorityLocal  = 0
	priorityRemote = 1
)

func BuildEndpointMap(destinations core_xds.DestinationMap,
	zoneEgress *core_mesh.ZoneEgressResource,
	dataplanes []*core_mesh.DataplaneResource,
	externalServices []*core_mesh.ExternalServiceResource) core_xds.EndpointMap {
	if len(destinations) == 0 {
		return nil
	}
	outbound := core_xds.EndpointMap{}
	for _, dataplane := range dataplanes {
		for _, inbound := range dataplane.Spec.GetNetworking().GetHealthyInbounds() {
			service := inbound.Tags[mesh_proto.ServiceTag]
			selectors, ok := destinations[service]
			if !ok {
				continue
			}
			withMesh := envoy.Tags(inbound.Tags).WithTags("mesh", dataplane.GetMeta().GetMesh())
			if !selectors.Matches(withMesh) {
				continue
			}
			iface := dataplane.Spec.Networking.ToInboundInterface(inbound)
			outbound[service] = append(outbound[service], core_xds.Endpoint{
				Target: iface.DataplaneIP,
				Port:   iface.DataplanePort,
				Tags:   withMesh,
				Weight: 1,
			})
		}
	}

	for _, externalService := range externalServices {
		serviceTags := externalService.Spec.GetTags()
		serviceName := serviceTags[mesh_proto.ServiceTag]
		locality := localityFromTags(priorityRemote, serviceTags)

		zeNetworking := zoneEgress.Spec.GetNetworking()
		zeAddress := zeNetworking.GetAddress()
		zePort := zeNetworking.GetPort()

		endpoint := core_xds.Endpoint{
			Target: zeAddress,
			Port:   zePort,
			Tags:   serviceTags,
			// AS it's a role of zone egress to load balance traffic between
			// instances, we can safely set weight to 1
			Weight:          1,
			Locality:        locality,
			ExternalService: &core_xds.ExternalService{},
		}

		outbound[serviceName] = append(outbound[serviceName], endpoint)

	}

	return outbound
}

func localityFromTags(priority uint32, tags map[string]string) *core_xds.Locality {
	zone, zonePresent := tags[mesh_proto.ZoneTag]

	if !zonePresent {
		// this means that we are running in standalone since in multi-zone Kuma always adds Zone tag automatically
		return nil
	}

	priority = priorityLocal

	return &core_xds.Locality{
		Zone:     zone,
		Priority: priority,
	}
}
