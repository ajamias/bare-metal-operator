/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package task

import (
	"context"
	"fmt"
	"time"

	"github.com/ajamias/bare-metal-operator/internal/workflow"
)

func init() {
	workflow.RegisterTask(&deployControlPlane{})
	workflow.RegisterTask(&drainNodes{})
	workflow.RegisterTask(&removeFromCluster{})
}

// deployControlPlane deploys the cluster control plane
type deployControlPlane struct{}

func (t *deployControlPlane) Run(ctx context.Context) error {
	fmt.Println("Deploying control plane components...")
	time.Sleep(300 * time.Millisecond)
	return nil
}

// drainNodes drains nodes before removal
type drainNodes struct{}

func (t *drainNodes) Run(ctx context.Context) error {
	fmt.Println("Draining nodes...")
	time.Sleep(200 * time.Millisecond)
	return nil
}

// removeFromCluster removes nodes from the cluster
type removeFromCluster struct{}

func (t *removeFromCluster) Run(ctx context.Context) error {
	fmt.Println("Removing nodes from cluster...")
	time.Sleep(150 * time.Millisecond)
	return nil
}
