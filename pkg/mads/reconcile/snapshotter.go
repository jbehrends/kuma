package reconcile

import (
	"context"

	envoy_core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	mesh_core "github.com/Kong/kuma/pkg/core/resources/apis/mesh"
	core_manager "github.com/Kong/kuma/pkg/core/resources/manager"
	core_store "github.com/Kong/kuma/pkg/core/resources/store"
	core_xds "github.com/Kong/kuma/pkg/core/xds"
	mads_cache "github.com/Kong/kuma/pkg/mads/cache"
	util_xds "github.com/Kong/kuma/pkg/util/xds"

	"github.com/Kong/kuma/pkg/mads/generator"
)

func NewSnapshotter(resourceManager core_manager.ResourceManager, generator generator.ResourceGenerator) Snapshotter {
	return &snapshotter{
		resourceManager: resourceManager,
		generator:       generator,
	}
}

type snapshotter struct {
	resourceManager core_manager.ResourceManager
	generator       generator.ResourceGenerator
}

func (s snapshotter) Snapshot(ctx context.Context, _ *envoy_core.Node) (util_xds.Snapshot, error) {
	meshes, err := s.getMeshes(ctx)
	if err != nil {
		return nil, err
	}

	dataplanes, err := s.getDataplanes(ctx, meshes)
	if err != nil {
		return nil, err
	}

	args := generator.Args{
		Meshes:     meshes,
		Dataplanes: dataplanes,
	}

	resources, err := s.generator.Generate(args)
	if err != nil {
		return nil, err
	}

	snapshot := mads_cache.NewSnapshot("", core_xds.ResourceList(resources).ToIndex())
	return &snapshot, nil
}

func (s snapshotter) getMeshes(ctx context.Context) ([]*mesh_core.MeshResource, error) {
	meshList := &mesh_core.MeshResourceList{}
	if err := s.resourceManager.List(ctx, meshList); err != nil {
		return nil, err
	}

	meshes := make([]*mesh_core.MeshResource, 0)
	for _, mesh := range meshList.Items {
		if mesh.HasPrometheusMetricsEnabled() {
			meshes = append(meshes, mesh)
		}
	}
	return meshes, nil
}

func (s snapshotter) getDataplanes(ctx context.Context, meshes []*mesh_core.MeshResource) ([]*mesh_core.DataplaneResource, error) {
	dataplanes := make([]*mesh_core.DataplaneResource, 0)
	for _, mesh := range meshes {
		dataplaneList := &mesh_core.DataplaneResourceList{}
		if err := s.resourceManager.List(ctx, dataplaneList, core_store.ListByMesh(mesh.Meta.GetName())); err != nil {
			return nil, err
		}
		dataplanes = append(dataplanes, dataplaneList.Items...)
	}
	return dataplanes, nil
}
