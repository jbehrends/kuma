package sync

import (
	"context"
	"sort"

	"github.com/kumahq/kuma/pkg/core/dns/lookup"
	core_mesh "github.com/kumahq/kuma/pkg/core/resources/apis/mesh"
	"github.com/kumahq/kuma/pkg/core/resources/manager"
	core_model "github.com/kumahq/kuma/pkg/core/resources/model"
	"github.com/kumahq/kuma/pkg/core/resources/registry"
	core_store "github.com/kumahq/kuma/pkg/core/resources/store"
	"github.com/kumahq/kuma/pkg/core/xds"
	"github.com/kumahq/kuma/pkg/xds/envoy"
	"github.com/kumahq/kuma/pkg/xds/ingress"
	xds_topology "github.com/kumahq/kuma/pkg/xds/topology"
)

type IngressProxyBuilder struct {
	ResManager         manager.ResourceManager
	ReadOnlyResManager manager.ReadOnlyResourceManager
	LookupIP           lookup.LookupIPFunc
	MetadataTracker    DataplaneMetadataTracker

	apiVersion envoy.APIVersion
	zone       string
}

func (p *IngressProxyBuilder) build(key core_model.ResourceKey) (*xds.Proxy, error) {
	ctx := context.Background()

	zoneIngress, err := p.getZoneIngress(key)
	if err != nil {
		return nil, err
	}
	zoneIngress, err = xds_topology.ResolveZoneIngressPublicAddress(p.LookupIP, zoneIngress)
	if err != nil {
		return nil, err
	}

	var zoneEgressList core_mesh.ZoneEgressResourceList
	if err := p.ReadOnlyResManager.List(ctx, &zoneEgressList); err != nil {
		return nil, err
	}

	// We want only our local egress
	var localZoneEgress *core_mesh.ZoneEgressResource
	for _, zoneEgress := range zoneEgressList.Items {
		if zoneEgress.IsLocalEgress(p.zone) {
			localZoneEgress = zoneEgress
			break
		}
	}

	allMeshDataplanes := &core_mesh.DataplaneResourceList{}
	if err := p.ReadOnlyResManager.List(ctx, allMeshDataplanes); err != nil {
		return nil, err
	}
	allMeshDataplanes.Items = xds_topology.ResolveAddresses(syncLog, p.LookupIP, allMeshDataplanes.Items)

	allMeshExternalServices := &core_mesh.ExternalServiceResourceList{}
	if err := p.ReadOnlyResManager.List(ctx, allMeshExternalServices); err != nil {
		return nil, err
	}

	externalServices := allMeshExternalServices.Items
	sort.Slice(externalServices, func(a, b int) bool {
		return externalServices[a].GetMeta().GetName() < externalServices[b].GetMeta().GetName()
	})

	routing := p.resolveRouting(zoneIngress, localZoneEgress, allMeshDataplanes, allMeshExternalServices)

	zoneIngressProxy, err := p.buildZoneIngressProxy(ctx)
	if err != nil {
		return nil, err
	}

	proxy := &xds.Proxy{
		Id:               xds.FromResourceKey(key),
		APIVersion:       p.apiVersion,
		ZoneIngress:      zoneIngress,
		Metadata:         p.MetadataTracker.Metadata(key),
		Routing:          *routing,
		ZoneIngressProxy: zoneIngressProxy,
	}
	return proxy, nil
}

func (p *IngressProxyBuilder) buildZoneIngressProxy(ctx context.Context) (*xds.ZoneIngressProxy, error) {
	routes := &core_mesh.TrafficRouteResourceList{}
	if err := p.ReadOnlyResManager.List(ctx, routes); err != nil {
		return nil, err
	}

	gatewayRoutes := &core_mesh.MeshGatewayRouteResourceList{}
	if _, err := registry.Global().DescriptorFor(core_mesh.MeshGatewayRouteType); err == nil { // GatewayRoute may not be registered
		if err := p.ReadOnlyResManager.List(ctx, gatewayRoutes); err != nil {
			return nil, err
		}
	}

	return &xds.ZoneIngressProxy{
		TrafficRouteList: routes,
		GatewayRoutes:    gatewayRoutes,
	}, nil
}

func (p *IngressProxyBuilder) getZoneIngress(key core_model.ResourceKey) (*core_mesh.ZoneIngressResource, error) {
	ctx := context.Background()

	zoneIngress := core_mesh.NewZoneIngressResource()
	if err := p.ReadOnlyResManager.Get(ctx, zoneIngress, core_store.GetBy(key)); err != nil {
		return nil, err
	}
	// Update Ingress' Available Services
	// This was placed as an operation of DataplaneWatchdog out of the convenience.
	// Consider moving to the outside of this component (follow the pattern of updating VIP outbounds)
	if err := p.updateIngress(zoneIngress); err != nil {
		return nil, err
	}
	return zoneIngress, nil
}

func (p *IngressProxyBuilder) resolveRouting(zoneIngress *core_mesh.ZoneIngressResource,
	zoneEgress *core_mesh.ZoneEgressResource,
	dataplanes *core_mesh.DataplaneResourceList,
	externalServices *core_mesh.ExternalServiceResourceList) *xds.Routing {

	destinations := ingress.BuildDestinationMap(zoneIngress)
	endpoints := ingress.BuildEndpointMap(destinations, zoneEgress, dataplanes.Items, externalServices.Items)

	routing := &xds.Routing{
		OutboundTargets: endpoints,
	}
	return routing
}

func (p *IngressProxyBuilder) updateIngress(zoneIngress *core_mesh.ZoneIngressResource) error {
	ctx := context.Background()

	allMeshDataplanes := &core_mesh.DataplaneResourceList{}
	if err := p.ReadOnlyResManager.List(ctx, allMeshDataplanes); err != nil {
		return err
	}
	allMeshDataplanes.Items = xds_topology.ResolveAddresses(syncLog, p.LookupIP, allMeshDataplanes.Items)

	allMeshExternalServices := &core_mesh.ExternalServiceResourceList{}
	if err := p.ReadOnlyResManager.List(ctx, allMeshExternalServices); err != nil {
		return err
	}

	externalServices := allMeshExternalServices.Items
	sort.Slice(externalServices, func(a, b int) bool {
		return externalServices[a].GetMeta().GetName() < externalServices[b].GetMeta().GetName()
	})

	// Update Ingress' Available Services
	// This was placed as an operation of DataplaneWatchdog out of the convenience.
	// Consider moving to the outside of this component (follow the pattern of updating VIP outbounds)
	return ingress.UpdateAvailableServices(ctx, p.ResManager, zoneIngress, allMeshDataplanes.Items, externalServices)
}
