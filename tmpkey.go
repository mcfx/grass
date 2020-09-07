package grass

import (
	"sync"
	"time"
)

type TmpKey struct {
	Key  uint32
	Time uint
}

type TmpKeySet struct {
	Keys   []TmpKey
	Map    map[uint32]struct{}
	Mutex  sync.Mutex
	Expire uint
}

func (s *TmpKeySet) HasKey(key uint32) bool {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	tm := uint(time.Now().Unix())
	if len(s.Keys) > 0 {
		i := 0
		for i < len(s.Keys) && s.Keys[i].Time < tm-s.Expire {
			delete(s.Map, s.Keys[i].Key)
			i++
		}
		s.Keys = s.Keys[i:]
	}
	_, ok := s.Map[key]
	return ok
}

func (s *TmpKeySet) AddKey(key uint32) {
	s.Mutex.Lock()
	defer s.Mutex.Unlock()
	tm := uint(time.Now().Unix())
	s.Keys = append(s.Keys, TmpKey{Key: key, Time: tm})
	s.Map[key] = struct{}{}
}

func NewTmpKeySet(expire uint) *TmpKeySet {
	return &TmpKeySet{Expire: expire, Map: make(map[uint32]struct{})}
}
