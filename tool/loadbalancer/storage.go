package loadbalancer

import (
	"sort"
	"sync"
)

func NewStorage(changeCh chan<- string) *Storage {
	return &Storage{
		hosts:    new(sync.Map),
		changeCh: changeCh,
	}
}

type Storage struct {
	hosts    *sync.Map
	changeCh chan<- string
}

func (s *Storage) Put(key, value string) {
	oldValue, ok := s.hosts.Load(key)
	if !ok || oldValue.(string) != value {
		s.hosts.Store(key, value)
		s.changeCh <- "put"
	}
}

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
