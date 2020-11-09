package noderesources

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/api/v1/resource"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/apis/config/validation"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	schedutil "k8s.io/kubernetes/pkg/scheduler/util"
)

type NodeResources struct {
	handle        framework.Handle
	resourceNames resourceToValueMap
	scorers       []weightedScorer
}

var _ = framework.ScorePlugin(&NodeResources{})

// NodeResourcesName is the name of the plugin used in the plugin registry and configurations.
const NodeResourcesName = "NodeResources"

// NewNodeResources initializes a new plugin and returns it.
func NewNodeResources(nrArgs runtime.Object, h framework.Handle) (framework.Plugin, error) {
	args, ok := nrArgs.(*config.NodeResourcesArgs)
	if !ok {
		return nil, fmt.Errorf("want args to be of type NodeResourcesArgs, got %T", nrArgs)
	}

	if err := validation.ValidateNodeResourcesArgs(*args); err != nil {
		return nil, err
	}

	nr := &NodeResources{
		handle: h,
	}

	if part := args.Allocatable; part != nil {
		nr.scorers = append(nr.scorers, weightedScorer{
			prefer:    part.Prefer,
			countOn:   part.CountOn,
			weights:   resourceWeights(part.Resources),
			getValues: func(rv *resourceValues) resourceToValueMap { return rv.getAllocatable() },
		})
	}
	if part := args.Allocated; part != nil {
		nr.scorers = append(nr.scorers, weightedScorer{
			prefer:    part.Prefer,
			countOn:   part.CountOn,
			weights:   resourceWeights(part.Resources),
			getValues: func(rv *resourceValues) resourceToValueMap { return rv.getAllocated() },
		})
	}
	if part := args.Available; part != nil {
		nr.scorers = append(nr.scorers, weightedScorer{
			prefer:    part.Prefer,
			countOn:   part.CountOn,
			weights:   resourceWeights(part.Resources),
			getValues: func(rv *resourceValues) resourceToValueMap { return rv.getAvailable() },
		})
	}
	if part := args.Requested; part != nil {
		nr.scorers = append(nr.scorers, weightedScorer{
			prefer:    part.Prefer,
			countOn:   part.CountOn,
			weights:   resourceWeights(part.Resources),
			getValues: func(rv *resourceValues) resourceToValueMap { return rv.getRequested() },
		})
	}

	nr.resourceNames = make(resourceToValueMap)
	for _, scorer := range nr.scorers {
		for resource := range scorer.weights {
			nr.resourceNames[resource] = 0
		}
	}

	return nr, nil
}

func resourceWeights(resources []config.ResourceSpec) resourceToWeightMap {
	weights := make(resourceToWeightMap, len(resources))
	for _, resource := range resources {
		weights[v1.ResourceName(resource.Name)] = resource.Weight
	}
	return weights
}

// Name returns name of the plugin. It is used in logs, etc.
func (nr *NodeResources) Name() string {
	return NodeResourcesName
}

// Score invoked at the score extension point.
func (nr *NodeResources) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
	nodeInfo, err := nr.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	if err != nil {
		return 0, framework.NewStatus(framework.Error, fmt.Sprintf("getting node %q from Snapshot: %v", nodeName, err))
	}

	rv := &resourceValues{
		nodeInfo:      nodeInfo,
		pod:           pod,
		resourceNames: nr.resourceNames,
	}

	var score int64
	for _, scorer := range nr.scorers {
		score += scorer.score(rv)
	}
	score /= int64(len(nr.scorers))

	return score, nil
}

// ScoreExtensions of the Score plugin.
func (nr *NodeResources) ScoreExtensions() framework.ScoreExtensions {
	return nil
}

type weightedScorer struct {
	prefer    config.PreferNodeResource
	countOn   config.CountNodeResourceOn
	weights   resourceToWeightMap
	getValues func(*resourceValues) resourceToValueMap
}

func (ws weightedScorer) score(rv *resourceValues) int64 {
	unweighted := ws.getValues(rv)
	var score, weightSum int64

	for resource, weight := range ws.weights {
		wv := unweighted[resource] * weight
		weightSum += weight

		switch ws.countOn {
		case config.CountOnAbsoluteValue:
		case config.CountOnRatioToAllocatable:
			wv = wv * framework.MaxNodeScore / (rv.getAllocatable()[resource])
		case config.CountOnRatioToAvailable:
			wv = wv * framework.MaxNodeScore / (rv.getAvailable()[resource])
		}
	}

	score /= weightSum

	if ws.prefer == config.PreferLeast {
		return -score
	}
	return score
}

type resourceValues struct {
	nodeInfo                                                *framework.NodeInfo
	pod                                                     *v1.Pod
	resourceNames                                           resourceToValueMap
	allocatable, allocated, available, requested, remaining resourceToValueMap
}

func (rv *resourceValues) getAllocatable() resourceToValueMap {
	if rv.allocatable == nil {
		rv.allocatable = make(resourceToValueMap, len(rv.resourceNames))
		for resource := range rv.resourceNames {
			switch resource {
			case v1.ResourceCPU:
				rv.allocatable[resource] = rv.nodeInfo.Allocatable.MilliCPU
			case v1.ResourceMemory:
				rv.allocatable[resource] = rv.nodeInfo.Allocatable.Memory
			case v1.ResourceEphemeralStorage:
				rv.allocatable[resource] = rv.nodeInfo.Allocatable.EphemeralStorage
			default:
				if schedutil.IsScalarResourceName(resource) {
					rv.allocatable[resource] = rv.nodeInfo.Allocatable.ScalarResources[resource]
				}
			}
		}
	}

	return rv.allocatable
}

func (rv *resourceValues) getAllocated() resourceToValueMap {
	if rv.allocated == nil {
		rv.allocated = make(resourceToValueMap, len(rv.resourceNames))
		for resource := range rv.resourceNames {
			switch resource {
			case v1.ResourceCPU:
				rv.allocatable[resource] = rv.nodeInfo.NonZeroRequested.MilliCPU
			case v1.ResourceMemory:
				rv.allocatable[resource] = rv.nodeInfo.NonZeroRequested.Memory
			case v1.ResourceEphemeralStorage:
				rv.allocatable[resource] = rv.nodeInfo.NonZeroRequested.EphemeralStorage
			default:
				if schedutil.IsScalarResourceName(resource) {
					rv.allocatable[resource] = rv.nodeInfo.NonZeroRequested.ScalarResources[resource]
				}
			}
		}
	}

	return rv.allocated
}

func (rv *resourceValues) getAvailable() resourceToValueMap {
	if rv.available == nil {
		allocatable := rv.getAllocatable()
		allocated := rv.getAllocated()

		rv.available = make(resourceToValueMap, len(allocatable))
		for resource, allocatable := range rv.allocatable {
			if avail := allocatable - allocated[resource]; avail > 0 {
				rv.available[resource] = avail
			}
			rv.available[resource] = 0
		}
	}

	return rv.available
}

func (rv *resourceValues) getRequested() resourceToValueMap {
	if rv.requested == nil {
		rv.requested = make(resourceToValueMap, len(rv.resourceNames))
		for resName := range rv.resourceNames {
			rv.requested[resName] = resource.GetResourceRequest(rv.pod, resName)
		}
	}

	return rv.requested
}
