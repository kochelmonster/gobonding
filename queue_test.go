package gobonding_test

import (
	"container/heap"
	"log"
	"testing"

	"github.com/kochelmonster/gobonding"
)

func TestOrderOverflow(t *testing.T) {
	queue := gobonding.NewQueue()
	heap.Push(&queue, &gobonding.Chunk{Idx: 0, Size: 0})
	heap.Push(&queue, &gobonding.Chunk{Idx: 0xffff, Size: 0})
	heap.Push(&queue, &gobonding.Chunk{Idx: 0xffff - 1, Size: 0})
	heap.Push(&queue, &gobonding.Chunk{Idx: 1, Size: 0})
	CheckQueue(t, &queue, []uint16{0xffff - 1, 0xffff, 0, 1})

	queue = gobonding.NewQueue()
	heap.Push(&queue, &gobonding.Chunk{Idx: 0, Size: 0})
	heap.Push(&queue, &gobonding.Chunk{Idx: 1, Size: 0})
	heap.Push(&queue, &gobonding.Chunk{Idx: 0xffff - 1, Size: 0})
	heap.Push(&queue, &gobonding.Chunk{Idx: 0xffff, Size: 0})
	CheckQueue(t, &queue, []uint16{0xffff - 1, 0xffff, 0, 1})
}

func TestBorderLow(t *testing.T) {
	queue := gobonding.NewQueue()
	heap.Push(&queue, &gobonding.Chunk{Idx: 0x1000, Size: 0})
	heap.Push(&queue, &gobonding.Chunk{Idx: 0x1000 - 1, Size: 0})
	heap.Push(&queue, &gobonding.Chunk{Idx: 0x1000 + 1, Size: 0})
	CheckQueue(t, &queue, []uint16{0x1000 - 1, 0x1000, 0x1000 + 1})
}

func TestBorderHigh(t *testing.T) {
	queue := gobonding.NewQueue()
	heap.Push(&queue, &gobonding.Chunk{Idx: 0xf000, Size: 0})
	heap.Push(&queue, &gobonding.Chunk{Idx: 0xf000 - 1, Size: 0})
	heap.Push(&queue, &gobonding.Chunk{Idx: 0xf000 + 1, Size: 0})
	CheckQueue(t, &queue, []uint16{0xf000 - 1, 0xf000, 0xf000 + 1})
}

func CheckQueue(t *testing.T, queue *gobonding.PriorityQueue, order []uint16) {
	i := 0
	for len(*queue) > 0 {
		chunk := heap.Pop(queue).(*gobonding.Chunk)
		log.Println("chunk", chunk)
		if chunk.Idx != order[i] {
			t.Errorf("Wrong order: Expected %v got %v", order[i], chunk.Idx)
		}
		i++
	}
}
