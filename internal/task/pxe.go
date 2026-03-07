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
	workflow.RegisterTask(&configurePXEBoot{})
	workflow.RegisterTask(&waitForPXEBoot{})
	workflow.RegisterTask(&installOS{})
	workflow.RegisterTask(&installBootloader{})
	workflow.RegisterTask(&resetPXEConfig{})
}

// configurePXEBoot configures PXE boot settings
type configurePXEBoot struct {
	PXEServer  string `json:"pxeServer"`
	BootImage  string `json:"bootImage"`
	KernelArgs string `json:"kernelArgs"`
}

func (t *configurePXEBoot) Run(ctx context.Context) error {
	fmt.Printf("Configuring PXE boot from %s with image %s\n", t.PXEServer, t.BootImage)
	time.Sleep(100 * time.Millisecond)
	return nil
}

// waitForPXEBoot waits for the host to boot via PXE
type waitForPXEBoot struct{}

func (t *waitForPXEBoot) Run(ctx context.Context) error {
	fmt.Println("Waiting for PXE boot to complete...")
	time.Sleep(250 * time.Millisecond)
	return nil
}

// installOS installs the operating system
type installOS struct {
	BootImage string `json:"bootImage"`
}

func (t *installOS) Run(ctx context.Context) error {
	fmt.Printf("Installing OS from image: %s\n", t.BootImage)
	time.Sleep(500 * time.Millisecond)
	return nil
}

// installBootloader installs the bootloader
type installBootloader struct{}

func (t *installBootloader) Run(ctx context.Context) error {
	fmt.Println("Installing bootloader...")
	time.Sleep(100 * time.Millisecond)
	return nil
}

// resetPXEConfig resets PXE configuration to defaults
type resetPXEConfig struct{}

func (t *resetPXEConfig) Run(ctx context.Context) error {
	fmt.Println("Resetting PXE configuration...")
	return nil
}
