package common




type Set[T comparable] struct {
	items map[T]bool
}

func NewSet[T comparable](items ...T) Set[T] {
	set := Set[T]{map[T]bool{}}
	for _, item := range items {
		set.Add(item)
	}
	return set
}


func (s *Set[T])Add(item T)  {
	s.items[item] = true
}

func (s *Set[T])Remove(item T)  {
	delete(s.items, item)
}

func (s *Set[T])Contains(item T) bool {
	return s.items[item]
}

func (s *Set[T])Values() []T  {
	output := []T{}
	for key := range s.items {
		output = append(output, key)
	}
	return output
}