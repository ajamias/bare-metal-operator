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
	"os"
	"reflect"

	"gopkg.in/yaml.v3"
)

var (
	registeredWorkflows = map[string]Workflow{}
	registeredTasks     = map[string]Task{}
)

// RegisterWorkflow registers a workflow by name
func RegisterWorkflow(wf Workflow) {
	registeredWorkflows[wf.Name] = wf
}

// RegisterTask registers a task function by name
func RegisterTask(task Task) {
	if task == nil {
		return
	}

	t := reflect.TypeOf(task)
	name := t.Name()
	if name == "" {
		return
	}

	registeredTasks[name] = task
}

// LoadWorkflowsFromYAML loads workflows from a YAML file and registers them
func LoadWorkflowsFromYAML(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	decoder := yaml.NewDecoder(file)
	var workflows []Workflow
	err = decoder.Decode(&workflows)
	if err != nil {
		return err
	}

	for i := range workflows {
		RegisterWorkflow(workflows[i])
	}

	return nil
}
