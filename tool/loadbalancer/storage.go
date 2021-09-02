package loadbalancer

import (
	"sort"
	"sync"
)

// NewStorage creates a new Storage
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

// Put puts new value
func (s *Storage) Put(key, value string) {
	oldValue, ok := s.hosts.Load(key)
	if !ok || oldValue.(string) != value {
		s.hosts.Store(key, value)
		s.changeCh <- "put"
	}
}

// Remove removes the old value from storage
func (s *Storage) Remove(key string) {
	_, ok := s.hosts.Load(key)
	if ok {
		s.hosts.Delete(key)
		s.changeCh <- "remove"
	}
}

// GetHosts returns a sorted list of hosts
func (s *Storage) GetHosts() []string {
	// unique
	serverMap := make(map[string]bool)
	s.hosts.Range(func(key, host interface{}) bool {
		serverMap[host.(string)] = true
		return true
	})
	// sort
	results := make([]string, 0)
	for host := range serverMap {
		results = append(results, host)
	}
	sort.Strings(results)
	return results
}
