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
	workflow.RegisterTask(&provisionHostWithImage{})
	workflow.RegisterTask(&validateHardware{})
	workflow.RegisterTask(&configureBIOS{})
	workflow.RegisterTask(&powerOn{})
	workflow.RegisterTask(&powerOff{})
	workflow.RegisterTask(&waitForBoot{})
	workflow.RegisterTask(&runHealthCheck{})
	workflow.RegisterTask(&cleanupStorage{})
	workflow.RegisterTask(&resetBMC{})
	workflow.RegisterTask(&networkAttach{})
}

// provisionHostWithImage provisions a host using an image URL
type provisionHostWithImage struct {
	ImageURL string `json:"imageURL"`
}

func (t *provisionHostWithImage) Run(ctx context.Context) error {
	fmt.Printf("Provisioning host with image: %s\n", t.ImageURL)
	time.Sleep(100 * time.Millisecond)
	return nil
}

// validateHardware checks that the host hardware meets requirements
type validateHardware struct{}

func (t *validateHardware) Run(ctx context.Context) error {
	fmt.Println("Validating hardware specifications...")
	time.Sleep(50 * time.Millisecond)
	return nil
}

// configureBIOS sets BIOS settings for optimal operation
type configureBIOS struct{}

func (t *configureBIOS) Run(ctx context.Context) error {
	fmt.Println("Configuring BIOS settings...")
	time.Sleep(75 * time.Millisecond)
	return nil
}

// powerOn powers on the host
type powerOn struct{}

func (t *powerOn) Run(ctx context.Context) error {
	fmt.Println("Powering on host...")
	return nil
}

// powerOff powers off the host
type powerOff struct{}

func (t *powerOff) Run(ctx context.Context) error {
	fmt.Println("Powering off host...")
	return nil
}

// waitForBoot waits for the host to complete boot
type waitForBoot struct{}

func (t *waitForBoot) Run(ctx context.Context) error {
	fmt.Println("Waiting for host to boot...")
	time.Sleep(200 * time.Millisecond)
	return nil
}

// runHealthCheck performs post-boot health checks
type runHealthCheck struct{}

func (t *runHealthCheck) Run(ctx context.Context) error {
	fmt.Println("Running health checks...")
	time.Sleep(100 * time.Millisecond)
	return nil
}

// cleanupStorage wipes storage devices
type cleanupStorage struct{}

func (t *cleanupStorage) Run(ctx context.Context) error {
	fmt.Println("Cleaning up storage...")
	time.Sleep(150 * time.Millisecond)
	return nil
}

// resetBMC resets the BMC to factory defaults
type resetBMC struct{}

func (t *resetBMC) Run(ctx context.Context) error {
	fmt.Println("Resetting BMC...")
	return nil
}

// networkAttach attaches the host to the network
type networkAttach struct{}

func (t *networkAttach) Run(ctx context.Context) error {
	fmt.Println("Attaching host to network...")
	time.Sleep(50 * time.Millisecond)
	return nil
}
