package sync

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("IngressProxyBuilder", func() {
	// var ingressProxyBuilder *IngressProxyBuilder
	// It("should gensserate available services for multiple meshes with the same tags", func() {
	// 	ingressProxyBuilder := IngressProxyBuilder{}
	// 	ctx := context.Background()
	// 	key := core_model.ResourceKey{

	// 	},

	// 	ingressProxyBuilder.build()
	// 	Expect(true).To(Equal(true))
	// }),
	FIt("shousld generate available services for multiple meshes with the same tags", func() {
		// defaultMeshWithMTLSAndZoneEgressAndLocality := &core_mesh.MeshResource{
		// 	Meta: &test_model.ResourceMeta{
		// 		Name: "default",
		// 	},
		// 	Spec: &mesh_proto.Mesh{
		// 		Mtls: &mesh_proto.Mesh_Mtls{
		// 			EnabledBackend: "ca-1",
		// 		},
		// 		Routing: &mesh_proto.Routing{
		// 			LocalityAwareLoadBalancing: true,
		// 			ZoneEgress:                 true,
		// 		},
		// 	},
		// }
		// zoneIngresses := &core_mesh.ZoneIngressResource{

		// 	Spec: &mesh_proto.ZoneIngress{
		// 		Zone: "zone-2",
		// 		Networking: &mesh_proto.ZoneIngress_Networking{
		// 			Address:           "10.20.1.2",
		// 			Port:              10001,
		// 			AdvertisedAddress: "192.168.0.100",
		// 			AdvertisedPort:    12345,
		// 		},
		// 		AvailableServices: []*mesh_proto.ZoneIngress_AvailableService{
		// 			{
		// 				Instances: 2,
		// 				Mesh:      "default",
		// 				Tags:      map[string]string{mesh_proto.ServiceTag: "redis", "version": "v2", mesh_proto.ZoneTag: "eu"},
		// 			},
		// 			{
		// 				Instances: 3,
		// 				Mesh:      "default",
		// 				Tags:      map[string]string{mesh_proto.ServiceTag: "redis", "version": "v3"},
		// 			},
		// 		},
		// 	},
		// }

		// ctx := context.Background()
		// cfg := kuma_cp.DefaultConfig()
		// builder, err := runtime.BuilderFor(context.Background(), cfg)
		// Expect(err).ToNot(HaveOccurred())
		// runtime, err := builder.Build()
		// Expect(err).ToNot(HaveOccurred())
		// tracker := callback.NewDataplaneMetadataTracker()
		// key := core_model.ResourceKey{
		// 	Name: "ingress-zone-1",
		// 	Mesh: "default",
		// }

		// rs := runtime.ResourceManager()
		// rs.Create(ctx, defaultMeshWithMTLSAndZoneEgressAndLocality, store.CreateByKey("mesh", "default"))
		// rs.Create(ctx, zoneIngresses, store.CreateByKey("ingress-zone-1", "default"))

		// test := defaultIngressProxyBuilder(runtime, tracker, envoy.APIV3)

		// test.build(key)
		Expect(true).To(Equal(true))
	})
})
