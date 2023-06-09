// Copyright 2019-2020 Intel Corporation. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pagemigrate

import (
	"fmt"
	"sync"

	logger "github.com/containers/nri-plugins/pkg/log"
	"github.com/containers/nri-plugins/pkg/resmgr/cache"
	"github.com/containers/nri-plugins/pkg/resmgr/control"
)

const (
	// PageMigrationController is the name/domain of the page migration controller.
	PageMigrationController = cache.PageMigration
	// PageMigrationConfigPath is the configuration path for the page migration controller.
	PageMigrationConfigPath = "resource-manager.control." + PageMigrationController
	// PageMigrationDescription is the description for the page migration controller.
	PageMigrationDescription = "page migration controller"
)

// migration implements the controller for memory page migration.
type migration struct {
	cache      cache.Cache           // resource manager cache
	sync.Mutex                       // protect access from multiple goroutines
	containers map[string]*container // containers we migrate
	demoter    *demoter              // demoter adopted from topology-aware policy
}

//
// The resource manager serializes access to the cache during request
// processing, event processing, and configuration updates by locking
// the resource-manager for each of these. Since controller hooks are
// invoked either as part of processing a request or an event, access
// to the cache from hooks is properly serialized.
//
// Page scanning or migration on the other hand happen asynchronously
// from dedicated goroutines. In order to avoid having to serialize
// access to the cache for these, we track and cache locally just enough
// data about containers that we can perform these actions completely on
// our own, without the need to access the resource manager cache at all.
//
// An alternative would have been to duplicate what we had originally in
// the policy:
//  - introduce controller events akin to policy events
//  - have the resource-manager call controller event handlers with the
//    lock held
//  - periodically inject a controller event when we want to scan pages
//  - perform page scanning or demotion from the event handler with the
//    resource-manager lock held
//
// However that would have destroyed one of the goals of splitting page
// scanning and migration out to a controller of its own, which was to
// perform these potentially time consuming actions without blocking
// concurrent processing of requests or events.
//

// container is the per container data we track locally.
type container struct {
	id         string
	prettyName string
	cgroupDir  string
	pm         *cache.PageMigrate
}

// Our logger instance.
var log = logger.NewLogger(PageMigrationController)

// Our singleton page migration controller.
var singleton *migration

// getMigrationController returns our singleton controller instance.
func getMigrationController() *migration {
	if singleton == nil {
		singleton = &migration{
			containers: make(map[string]*container),
		}
		singleton.demoter = newDemoter(singleton)
	}
	return singleton
}

// Start prepares the controller for resource control/decision enforcement.
func (m *migration) Start(cache cache.Cache) error {
	m.cache = cache
	m.syncWithCache()
	m.demoter.Reconfigure()
	return nil
}

// Stop shuts down the controller.
func (m *migration) Stop() {
	m.demoter.Stop()
}

// PreCreateHook is the controller's pre-create hook.
func (m *migration) PreCreateHook(cache.Container) error {
	return nil
}

// PreStartHook is the controller's pre-start hook.
func (m *migration) PreStartHook(cache.Container) error {
	return nil
}

// PostStartHook is the controller's post-start hook.
func (m *migration) PostStartHook(cc cache.Container) error {
	m.Lock()
	defer m.Unlock()
	err := m.insertContainer(cc)
	cc.ClearPending(PageMigrationController)
	return err
}

// PostUpdateHook is the controller's post-update hook.
func (m *migration) PostUpdateHook(cc cache.Container) error {
	m.Lock()
	defer m.Unlock()
	m.updateContainer(cc)
	cc.ClearPending(PageMigrationController)
	return nil
}

// PostStopHook is the controller's post-stop hook.
func (m *migration) PostStopHook(cc cache.Container) error {
	m.Lock()
	defer m.Unlock()
	m.deleteContainer(cc)
	return nil
}

// TestHook is the memory controller testing hook.
func (m *migration) Test() {
}

// syncWithCache synchronizes tracked containers with the cache.
func (m *migration) syncWithCache() {
	m.Lock()
	defer m.Unlock()
	m.containers = make(map[string]*container)
	for _, cc := range m.cache.GetContainers() {
		m.insertContainer(cc)
	}
}

// insertContainer creates a local copy of the container.
func (m *migration) insertContainer(cc cache.Container) error {
	pm := cc.GetPageMigration()
	if pm == nil {
		return nil
	}

	c := &container{
		id:         cc.GetID(),
		prettyName: cc.PrettyName(),
		cgroupDir:  cc.GetCgroupDir(),
		pm:         pm.Clone(),
	}
	if c.cgroupDir == "" {
		return migrationError("can't find cgroup dir for container %s",
			c.prettyName)
	}

	m.containers[c.id] = c

	return nil
}

// updateContainer updates the local copy of the container.
func (m *migration) updateContainer(cc cache.Container) error {
	pm := cc.GetPageMigration()
	if pm == nil {
		delete(m.containers, cc.GetID())
		return nil
	}

	c, ok := m.containers[cc.GetID()]
	if !ok {
		return m.insertContainer(cc)
	}

	c.pm = pm.Clone()
	return nil
}

// deleteContainer creates a local copy of the container.
func (m *migration) deleteContainer(cc cache.Container) error {
	delete(m.containers, cc.GetID())
	return nil
}

// GetID replicates the respective cache.Container function.
func (c *container) GetID() string {
	return c.id
}

// GetCgroupDir replicates the respective cache.Container function.
func (c *container) GetCgroupDir() string {
	return c.GetCgroupDir()
}

// GetPageMigration replicates the respective cache.Container function.
func (c *container) GetPageMigration() *cache.PageMigrate {
	return c.pm
}

// PrettyName replicates the respective cache.Container function.
func (c *container) PrettyName() string {
	return c.prettyName
}

// init registers this controller.
func init() {
	control.Register(PageMigrationController, "page migration controller", getMigrationController())
}

// migrationError creates a controller-specific formatted error message.
func migrationError(format string, args ...interface{}) error {
	return fmt.Errorf("page-migrate: "+format, args...)
}
