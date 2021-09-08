/*
Copyright 2021 Gravitational, Inc.

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

package internal

import (
	"context"
	"sort"
	"sync"
)

// NewStorage creates a new Storage
// while the Storage is in use, the channel must be open and messages must be read
func NewStorage(changeCh chan<- string) *Storage {
	return &Storage{
		hosts:    new(sync.Map),
		changeCh: changeCh,
	}
}

// Storage stores current addresses for apiserver
type Storage struct {
	hosts    *sync.Map
	changeCh chan<- string
}

// Put stores a new value into this storage under the specified key.
// Previous value under the same key is overwritten.
func (s *Storage) Put(ctx context.Context, key, value string) {
	oldValue, ok := s.hosts.Load(key)
	if !ok || oldValue.(string) != value {
		s.hosts.Store(key, value)
		select {
		case s.changeCh <- "put":
			return
		case <-ctx.Done():

		}
	}
}

// Remove removes the old value from storage
func (s *Storage) Remove(ctx context.Context, key string) {
	_, ok := s.hosts.Load(key)
	if ok {
		s.hosts.Delete(key)
		select {
		case s.changeCh <- "remove":
			return
		case <-ctx.Done():

		}
	}
}

type void struct{}

// GetHosts returns a sorted list of hosts
func (s *Storage) GetHosts() []string {
	// unique
	serverMap := make(map[string]void)
	s.hosts.Range(func(key, host interface{}) bool {
		serverMap[host.(string)] = void{}
		return true
	})
	// sort
	results := make([]string, 0, len(serverMap))
	for host := range serverMap {
		results = append(results, host)
	}
	sort.Strings(results)
	return results
}
