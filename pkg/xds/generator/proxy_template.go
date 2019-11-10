package generator

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/pkg/errors"

	kuma_mesh "github.com/Kong/kuma/api/mesh/v1alpha1"
	model "github.com/Kong/kuma/pkg/core/xds"
	xds_context "github.com/Kong/kuma/pkg/xds/context"
	"github.com/Kong/kuma/pkg/xds/envoy"
	"github.com/Kong/kuma/pkg/xds/template"
	"github.com/ghodss/yaml"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/ptypes"
	any "github.com/golang/protobuf/ptypes/any"
)

type TemplateProxyGenerator struct {
	ProxyTemplate *kuma_mesh.ProxyTemplate
}

func (g *TemplateProxyGenerator) Generate(ctx xds_context.Context, proxy *model.Proxy) ([]*Resource, error) {
	resources := make([]*Resource, 0, len(g.ProxyTemplate.Imports)+1)
	for i, name := range g.ProxyTemplate.Imports {
		generator := &ProxyTemplateProfileSource{ProfileName: name}
		if rs, err := generator.Generate(ctx, proxy); err != nil {
			return nil, fmt.Errorf("imports[%d]{name=%q}: %s", i, name, err)
		} else {
			resources = append(resources, rs...)
		}
	}
	generator := &ProxyTemplateRawSource{Resources: g.ProxyTemplate.Resources}
	if rs, err := generator.Generate(ctx, proxy); err != nil {
		return nil, fmt.Errorf("resources: %s", err)
	} else {
		resources = append(resources, rs...)
	}
	return resources, nil
}

type ProxyTemplateRawSource struct {
	Resources []*kuma_mesh.ProxyTemplateRawResource
}

func (s *ProxyTemplateRawSource) Generate(_ xds_context.Context, proxy *model.Proxy) ([]*Resource, error) {
	resources := make([]*Resource, 0, len(s.Resources))
	for i, r := range s.Resources {
		json, err := yaml.YAMLToJSON([]byte(r.Resource))
		if err != nil {
			json = []byte(r.Resource)
		}

		var anything any.Any
		if err := (&jsonpb.Unmarshaler{}).Unmarshal(bytes.NewReader(json), &anything); err != nil {
			return nil, fmt.Errorf("raw.resources[%d]{name=%q}.resource: %s", i, r.Name, err)
		}
		var dyn ptypes.DynamicAny
		if err := ptypes.UnmarshalAny(&anything, &dyn); err != nil {
			return nil, fmt.Errorf("raw.resources[%d]{name=%q}.resource: %s", i, r.Name, err)
		}
		p, ok := dyn.Message.(ResourcePayload)
		if !ok {
			return nil, fmt.Errorf("raw.resources[%d]{name=%q}.resource: xDS resource doesn't implement all required interfaces", i, r.Name)
		}
		if v, ok := p.(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return nil, fmt.Errorf("raw.resources[%d]{name=%q}.resource: %s", i, r.Name, err)
			}
		}

		resources = append(resources, &Resource{
			Name:     r.Name,
			Version:  r.Version,
			Resource: p,
		})
	}
	return resources, nil
}

var predefinedProfiles = make(map[string]ResourceGenerator)

func NewDefaultProxyProfile() ResourceGenerator {
	return CompositeResourceGenerator{TransparentProxyGenerator{}, InboundProxyGenerator{}, OutboundProxyGenerator{}}
}

func init() {
	predefinedProfiles[template.ProfileDefaultProxy] = NewDefaultProxyProfile()
}

type ProxyTemplateProfileSource struct {
	ProfileName string
}

func (s *ProxyTemplateProfileSource) Generate(ctx xds_context.Context, proxy *model.Proxy) ([]*Resource, error) {
	g, ok := predefinedProfiles[s.ProfileName]
	if !ok {
		return nil, fmt.Errorf("profile{name=%q}: unknown profile", s.ProfileName)
	}
	return g.Generate(ctx, proxy)
}

type InboundProxyGenerator struct {
}

func (_ InboundProxyGenerator) Generate(ctx xds_context.Context, proxy *model.Proxy) ([]*Resource, error) {
	endpoints, err := proxy.Dataplane.Spec.Networking.GetInboundInterfaces()
	if err != nil {
		return nil, err
	}
	if len(endpoints) == 0 {
		return nil, nil
	}
	virtual := proxy.Dataplane.Spec.Networking.GetTransparentProxying().GetRedirectPort() != 0
	resources := make([]*Resource, 0, len(endpoints))
	names := make(map[string]bool)
	for _, endpoint := range endpoints {
		localClusterName := fmt.Sprintf("localhost:%d", endpoint.WorkloadPort)
		if used := names[localClusterName]; !used {
			resources = append(resources, &Resource{
				Name:     localClusterName,
				Version:  "",
				Resource: envoy.CreateLocalCluster(localClusterName, "127.0.0.1", endpoint.WorkloadPort),
			})
			names[localClusterName] = true
		}

		inboundListenerName := fmt.Sprintf("inbound:%s:%d", endpoint.DataplaneIP, endpoint.DataplanePort)
		if used := names[inboundListenerName]; !used {
			resources = append(resources, &Resource{
				Name:     inboundListenerName,
				Version:  "",
				Resource: envoy.CreateInboundListener(ctx, inboundListenerName, endpoint.DataplaneIP, endpoint.DataplanePort, localClusterName, virtual, proxy.TrafficPermissions.Get(endpoint.String()), proxy.Metadata),
			})
			names[inboundListenerName] = true
		}
	}
	return resources, nil
}

type OutboundProxyGenerator struct {
}

func (_ OutboundProxyGenerator) Generate(ctx xds_context.Context, proxy *model.Proxy) ([]*Resource, error) {
	ofaces := proxy.Dataplane.Spec.Networking.GetOutbound()
	if len(ofaces) == 0 {
		return nil, nil
	}
	virtual := proxy.Dataplane.Spec.Networking.GetTransparentProxying().GetRedirectPort() != 0
	resources := make([]*Resource, 0, len(ofaces))
	names := make(map[string]bool)
	sourceService := proxy.Dataplane.Spec.GetIdentifyingService()
	for i, oface := range ofaces {
		endpoint, err := kuma_mesh.ParseOutboundInterface(oface.Interface)
		if err != nil {
			return nil, err
		}

		route := proxy.TrafficRoutes[oface.Service]
		if route == nil {
			return nil, errors.Errorf("outbound interface [%d]{service=%q} has no TrafficRoute", i, oface.Service)
		}

		var clusters []envoy.ClusterInfo
		for _, destination := range route.Spec.Conf {
			service, ok := destination.Destination[kuma_mesh.ServiceTag]
			if !ok {
				// all destinations must have `service` tag
				continue
			}
			if destination.Weight == 0 {
				// Envoy doesn't support 0 weight
				continue
			}
			clusters = append(clusters, envoy.ClusterInfo{
				Name:   destinationClusterName(service, destination.Destination),
				Weight: destination.Weight,
				Tags:   destination.Destination,
			})
		}

		for _, cluster := range clusters {
			if used := names[cluster.Name]; !used {
				resources = append(resources, &Resource{
					Name:     cluster.Name,
					Resource: envoy.CreateEdsCluster(ctx, cluster.Name, proxy.Metadata),
				})
				endpoints := model.EndpointList(proxy.OutboundTargets[cluster.Tags[kuma_mesh.ServiceTag]]).Filter(kuma_mesh.MatchTags(cluster.Tags))
				resources = append(resources, &Resource{
					Name:     cluster.Name,
					Resource: envoy.CreateClusterLoadAssignment(cluster.Name, endpoints),
				})
				names[cluster.Name] = true
			}
		}

		outboundListenerName := fmt.Sprintf("outbound:%s:%d", endpoint.DataplaneIP, endpoint.DataplanePort)
		if used := names[outboundListenerName]; !used {
			destinationService := oface.Service
			listener, err := envoy.CreateOutboundListener(ctx, outboundListenerName, endpoint.DataplaneIP, endpoint.DataplanePort, oface.Service, clusters, virtual, sourceService, destinationService, proxy.Logs.Outbounds[oface.Interface], proxy)
			if err != nil {
				return nil, errors.Wrapf(err, "could not generate listener %s", outboundListenerName)
			}
			resources = append(resources, &Resource{
				Name:     outboundListenerName,
				Resource: listener,
			})
			names[outboundListenerName] = true
		}
	}
	return resources, nil
}

type TransparentProxyGenerator struct {
}

func (_ TransparentProxyGenerator) Generate(ctx xds_context.Context, proxy *model.Proxy) ([]*Resource, error) {
	redirectPort := proxy.Dataplane.Spec.Networking.GetTransparentProxying().GetRedirectPort()
	if redirectPort == 0 {
		return nil, nil
	}
	return []*Resource{
		&Resource{
			Name:     "catch_all",
			Version:  proxy.Dataplane.Meta.GetVersion(),
			Resource: envoy.CreateCatchAllListener(ctx, "catch_all", "0.0.0.0", redirectPort, "pass_through"),
		},
		&Resource{
			Name:     "pass_through",
			Version:  proxy.Dataplane.Meta.GetVersion(),
			Resource: envoy.CreatePassThroughCluster("pass_through"),
		},
	}, nil
}

func destinationClusterName(service string, selector map[string]string) string {
	var pairs []string
	for key, value := range selector {
		if key == kuma_mesh.ServiceTag {
			continue
		}
		pairs = append(pairs, fmt.Sprintf("%s=%s", key, value))
	}
	if len(pairs) == 0 {
		return service
	}
	sort.Strings(pairs)
	return fmt.Sprintf("%s{%s}", service, strings.Join(pairs, ","))
}
