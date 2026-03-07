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
	workflow.RegisterTask(&configureRAID{})
	workflow.RegisterTask(&partitionStorage{})
	workflow.RegisterTask(&writeImageToStorage{})
}

// configureRAID sets up RAID configuration on the host
type configureRAID struct{}

func (t *configureRAID) Run(ctx context.Context) error {
	fmt.Println("Configuring RAID arrays...")
	time.Sleep(200 * time.Millisecond)
	return nil
}

// partitionStorage creates disk partitions
type partitionStorage struct {
	StorageDevice string `json:"storageDevice"`
}

func (t *partitionStorage) Run(ctx context.Context) error {
	fmt.Printf("Partitioning storage device: %s\n", t.StorageDevice)
	time.Sleep(100 * time.Millisecond)
	return nil
}

// writeImageToStorage writes an OS image to storage
type writeImageToStorage struct {
	ImageURL      string `json:"imageURL"`
	StorageDevice string `json:"storageDevice"`
}

func (t *writeImageToStorage) Run(ctx context.Context) error {
	fmt.Printf("Writing image %s to %s\n", t.ImageURL, t.StorageDevice)
	time.Sleep(300 * time.Millisecond)
	return nil
}
