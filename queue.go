package gobonding

import "container/heap"

type PriorityQueue []*Chunk

func NewQueue() PriorityQueue {
	queue := make(PriorityQueue, 0)
	heap.Init(&queue)
	return queue
}

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
	if pq[i].Idx < 0x1000 && pq[j].Idx > 0xf000 {
		// Overflow case
		return false
	}
	if pq[j].Idx < 0x1000 && pq[i].Idx > 0xf000 {
		// Overflow case
		return true
	}
	return pq[i].Idx < pq[j].Idx
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *PriorityQueue) Push(x any) {
	*pq = append(*pq, x.(*Chunk))
}

func (pq *PriorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	*pq = old[0 : n-1]
	return item
}
