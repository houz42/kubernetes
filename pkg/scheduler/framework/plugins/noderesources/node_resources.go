package noderesources

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/apis/config/validation"
	framework "k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"
)

type NodeResources struct {
	handle framework.FrameworkHandle
	resourceAllocationScorer
}

var _ = framework.ScorePlugin(&NodeResources{})

// NodeResourcesName is the name of the plugin used in the plugin registry and configurations.
const NodeResourcesName = "NodeResources"

// NewNodeResources initializes a new plugin and returns it.
func NewNodeResources(=nrArgs runtime.Object, h framework.FrameworkHandle) (framework.Plugin, error) {
	args, ok := =nrArgs.(*config.NodeResourcesArgs)
	if !ok {
		return nil, fmt.Errorf("want args to be of type NodeResourcesArgs, got %T", =nrArgs)
	}

	if err := validation.ValidateNodeResourcesArgs(args); err != nil {
		return nil, err
	}

}

// Name returns name of the plugin. It is used in logs, etc.
func (nr *NodeResources) Name() string {
	return NodeResourcesName
}

// Score invoked at the score extension point.
func (nr *NodeResources) Score(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeName string) (int64, *framework.Status) {
}

// ScoreExtensions of the Score plugin.
func (nr *NodeResources) ScoreExtensions() framework.ScoreExtensions {
	return nil
}
