package noderesourcesfitplus

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/api/v1/resource"
	k8sConfig "k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	plfeature "k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/noderesources"
	"sigs.k8s.io/scheduler-plugins/apis/config"
)

const (
	// Name is plugin name
	Name = "NodeResourcesFitPlus"
)

var (
	_ framework.ScorePlugin = &Plugin{}
)

type Plugin struct {
	handle framework.Handle
	args   *config.NodeResourcesFitPlusArgs
}

func New(_ context.Context, args runtime.Object, handle framework.Handle) (framework.Plugin, error) {

	nodeResourcesFitPlusArgs, ok := args.(*config.NodeResourcesFitPlusArgs)

	if !ok {
		return nil, fmt.Errorf("want args to be of type NodeResourcesFitPlusArgs, got %T", args)
	}

	return &Plugin{
		handle: handle,
		args:   nodeResourcesFitPlusArgs,
	}, nil
}

func (s *Plugin) Name() string {
	return Name
}

func (s *Plugin) Score(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) (int64, *framework.Status) {
	nodeInfo, err := s.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	if err != nil {
		return 0, framework.NewStatus(framework.Error, fmt.Sprintf("getting node %q from Snapshot: %v", nodeName, err))
	}

	var nodeScore int64
	var weightSum int64

	podRequest, _ := fitsRequest(computePodResourceRequest(p).Resource, nodeInfo)

	for _, requestSourceName := range podRequest {
		v, ok := s.args.Resources[requestSourceName]
		if !ok {
			continue
		}
		fit, err := noderesources.NewFit(ctx,
			&k8sConfig.NodeResourcesFitArgs{
				ScoringStrategy: &k8sConfig.ScoringStrategy{
					Type: v.Type, // MostAllocated or LeastAllocated
					Resources: []k8sConfig.ResourceSpec{
						{Name: string(requestSourceName), Weight: 1},
					},
				},
			}, s.handle, plfeature.Features{})

		if err != nil {
			return 0, framework.NewStatus(framework.Error, err.Error())
		}
		resourceScore, status := fit.(framework.ScorePlugin).Score(ctx, state, p, nodeName)
		if !status.IsSuccess() {
			return 0, framework.NewStatus(framework.Error, err.Error())
		}

		nodeScore += resourceScore * v.Weight
		weightSum += v.Weight
	}

	if weightSum == 0 {
		return framework.MaxNodeScore, framework.NewStatus(framework.Success, "")
	}
	scores := nodeScore / weightSum

	return scores, nil
}

func (p *Plugin) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

type preFilterState struct {
	framework.Resource
}

func computePodResourceRequest(pod *v1.Pod) *preFilterState {
	// pod hasn't scheduled yet so we don't need to worry about InPlacePodVerticalScalingEnabled
	reqs := resource.PodRequests(pod, resource.PodResourcesOptions{})
	result := &preFilterState{}
	result.SetMaxResource(reqs)
	return result
}

func fitsRequest(podRequest framework.Resource, nodeInfo *framework.NodeInfo) ([]v1.ResourceName, []v1.ResourceName) {
	var podRequestResource []v1.ResourceName
	var nodeRequestResource []v1.ResourceName

	if podRequest.MilliCPU > 0 {
		podRequestResource = append(podRequestResource, v1.ResourceCPU)
	}

	if nodeInfo.Allocatable.MilliCPU > 0 {
		nodeRequestResource = append(nodeRequestResource, v1.ResourceCPU)
	}

	if podRequest.Memory > 0 {
		podRequestResource = append(podRequestResource, v1.ResourceMemory)
	}

	if nodeInfo.Allocatable.Memory > 0 {
		nodeRequestResource = append(nodeRequestResource, v1.ResourceMemory)
	}

	if podRequest.EphemeralStorage > 0 {
		podRequestResource = append(podRequestResource, v1.ResourceEphemeralStorage)
	}

	if nodeInfo.Allocatable.EphemeralStorage > 0 {
		nodeRequestResource = append(nodeRequestResource, v1.ResourceEphemeralStorage)
	}

	for rName, rQuant := range podRequest.ScalarResources {
		if rQuant > 0 {
			podRequestResource = append(podRequestResource, rName)
		}
	}

	for rName, rQuant := range nodeInfo.Allocatable.ScalarResources {
		if rQuant > 0 {
			nodeRequestResource = append(nodeRequestResource, rName)
		}
	}

	return podRequestResource, nodeRequestResource
}
