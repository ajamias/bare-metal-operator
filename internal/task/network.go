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
	workflow.RegisterTask(&configureNetwork{})
	workflow.RegisterTask(&configureClusterNetworking{})
}

// configureNetwork sets up network configuration on the host
type configureNetwork struct {
	NetworkConfig string `json:"networkConfig"`
}

func (t *configureNetwork) Run(ctx context.Context) error {
	fmt.Printf("Configuring network with config: %s\n", t.NetworkConfig)
	time.Sleep(100 * time.Millisecond)
	return nil
}

// configureClusterNetworking sets up cluster-wide networking
type configureClusterNetworking struct{}

func (t *configureClusterNetworking) Run(ctx context.Context) error {
	fmt.Println("Configuring cluster networking...")
	time.Sleep(150 * time.Millisecond)
	return nil
}
