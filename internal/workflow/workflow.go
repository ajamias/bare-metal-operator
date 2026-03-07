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

package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"github.com/ajamias/bare-metal-operator/api/v1alpha1"
)

// Run runs a workflow phase for the specific event
func RunOnEvent(ctx context.Context, wf v1alpha1.Workflow, event Event) error {
	err := Validate(wf)
	if err != nil {
		return err
	}

	workflow := registeredWorkflows[wf.Name]

	var phases []Phase
	switch event {
	case EventHostCreate:
		phases = workflow.SetUpHost
	case EventHostDelete:
		phases = workflow.TearDownHost
	case EventClusterCreate:
		phases = workflow.SetUpCluster
	case EventClusterDelete:
		phases = workflow.TearDownCluster
	default:
		return errors.New("invalid workflow event")
	}

	// Build JSON bytes directly from raw JSON values
	inputMap := make(map[string]json.RawMessage, len(wf.Input))
	for key, jsonVal := range wf.Input {
		inputMap[key] = json.RawMessage(jsonVal.Raw)
	}

	inputBytes, err := json.Marshal(inputMap)
	if err != nil {
		return err
	}

	for i := range phases {
		if err := phases[i].Run(ctx, inputBytes); err != nil {
			return err
		}
	}

	return nil
}

// Validate verifies that a workflow exists and has valid inputs
func Validate(wf v1alpha1.Workflow) error {
	workflow, ok := registeredWorkflows[wf.Name]
	if !ok {
		return errors.New("invalid workflow")
	}

	for key, expectedType := range workflow.ExpectedInput {
		ok := validateInputType(wf.Input[key], expectedType)
		if !ok {
			return errors.New("invalid input type")
		}
	}

	return nil
}

func validateInputType(input apiextensionsv1.JSON, expectedType string) bool {
	var value any
	if err := json.Unmarshal(input.Raw, &value); err != nil {
		return false
	}

	t := reflect.TypeOf(value)
	if t == nil {
		return expectedType == "nil"
	}

	return t.String() == expectedType
}

// Event represents the type of event that triggers a phase
type Event int

const (
	EventUnknown Event = iota
	EventHostCreate
	EventHostDelete
	EventClusterCreate
	EventClusterDelete
)

// Phase represents a set of tasks to perform
type Phase struct {
	Name     string   `yaml:"name"`
	Parallel bool     `yaml:"parallel"`
	Tasks    []string `yaml:"tasks"`
}

func (p *Phase) Run(ctx context.Context, inputBytes []byte) error {
	if p.Parallel {
		var wg sync.WaitGroup
		errChan := make(chan error, len(p.Tasks))
		for i := range p.Tasks {
			wg.Add(1)
			go func(taskName string) {
				defer wg.Done()
				task := registeredTasks[taskName]

				err := json.Unmarshal(inputBytes, &task)
				if err != nil {
					errChan <- err
				}

				err = task.Run(ctx)
				if err != nil {
					errChan <- err
				}
			}(p.Tasks[i])
		}
		wg.Wait()
		close(errChan)

		// Return the first error if any
		for err := range errChan {
			if err != nil {
				return err
			}
		}
	} else {
		for i := range p.Tasks {
			task := registeredTasks[p.Tasks[i]]

			err := json.Unmarshal(inputBytes, &task)
			if err != nil {
				return err
			}

			err = task.Run(ctx)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// Task represents a task with its name and function
type Task interface {
	Run(context.Context) error
}

// Workflow represents a complete workflow with phases
type Workflow struct {
	Name              string            `yaml:"name"`
	MatchHosts        map[string]string `yaml:"matchHosts"`
	SetHostProperties map[string]string `yaml:"setHostProperties"`
	ExpectedInput     map[string]string `yaml:"expectedInput"`

	SetUpHost       []Phase `yaml:"setUpHost"`
	SetUpCluster    []Phase `yaml:"setUpCluster"`
	TearDownHost    []Phase `yaml:"tearDownHost"`
	TearDownCluster []Phase `yaml:"tearDownCluster"`
}
