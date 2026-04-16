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

// Package profile provides profile loading and matching functionality
package profile

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

var (
	registeredProfiles = map[string]*Profile{}
)

// LoadProfilesFromFile reads profiles from a YAML file and returns them as a map
func LoadProfilesFromFile(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	var profiles []*Profile
	decoder := yaml.NewDecoder(file)
	if err := decoder.Decode(&profiles); err != nil {
		return err
	}

	for _, profile := range profiles {
		if profile.Name == "" {
			return fmt.Errorf("profile missing required 'name' field")
		}
		if _, exists := registeredProfiles[profile.Name]; exists {
			return fmt.Errorf("duplicate profile name: %s", profile.Name)
		}
		registeredProfiles[profile.Name] = profile
	}

	return nil
}

// Get returns a registered profile or nil if it doesn't exist
func Get(name string) *Profile {
	return registeredProfiles[name]
}

// Profile defines a configuration profile with workflows and host selection
type Profile struct {
	Name                  string            `yaml:"name"`
	HostSelector          map[string]string `yaml:"hostSelector"`
	BareMetalPoolTemplate string            `yaml:"bareMetalPoolTemplate,omitempty"`
	HostTemplate          string            `yaml:"hostTemplate,omitempty"`
}
