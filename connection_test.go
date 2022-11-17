package gobonding_test

import (
	"context"
	"log"
	"sort"
	"testing"
	"time"

	"github.com/kochelmonster/gobonding"
)

func createConnManager(ctx context.Context) *gobonding.ConnManager {
	config := gobonding.Config{}
	return gobonding.NewConnMananger(ctx, &config)
}

func TestOrdering(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cm := createConnManager(ctx)

	InAndOut(t, cm, []uint16{0, 1})
	if cm.PeerOrder != 2 {
		t.Fatalf("Wrong Peer order expected 2 got %v", cm.PeerOrder)
	}

	InAndOut(t, cm, []uint16{4, 3, 2})
	if cm.PeerOrder != 5 {
		t.Fatalf("Wrong Peer order expected 5 got %v", cm.PeerOrder)
	}

	InAndOut(t, cm, []uint16{7, 10, 6, 9, 8})
	if cm.PeerOrder != 11 {
		t.Fatalf("Wrong Peer order expected 6 got %v", cm.PeerOrder)
	}

	cancel()
	time.Sleep(100 * time.Nanosecond)
}

func InAndOut(t *testing.T, cm *gobonding.ConnManager, idxs []uint16) []int {
	result := make([]int, len(idxs))
	for _, idx := range idxs {
		cm.CollectChannel <- &gobonding.Chunk{Idx: idx}
	}
	for i := range idxs {
		chunk := <-cm.OrderedChannel
		log.Println("test", chunk)
		result[i] = int(chunk.Idx)
	}
	if !sort.IntsAreSorted(result) {
		t.Fatalf("Wrong Chunk order %v", result)
	}
	return result
}

func TestAllocAndFree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cm := createConnManager(ctx)

	chunk1 := cm.AllocChunk()
	chunk2 := cm.AllocChunk()
	chunk1.Idx = 1
	chunk2.Idx = 2
	cm.FreeChunk(chunk1)

	chunk3 := cm.AllocChunk()
	if chunk3.Idx != 1 {
		t.Fatalf("Chunk not reused")
	}

	cm.FreeChunk(chunk3)
	cm.FreeChunk(chunk2)

	cancel()
}

func TestSyncAndClear(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cm := createConnManager(ctx)

	for i := 1; i <= 5; i++ {
		cm.DispatchChannel <- &gobonding.Chunk{Idx: uint16(i)}
	}
	cm.SyncCounter()
	if len(cm.DispatchChannel) != 1 {
		t.Fatalf("Channel not freed")
	}

	msg := <-cm.DispatchChannel
	sc := msg.(*gobonding.SyncOrderMsg)
	if sc.Order != 0 {
		t.Fatalf("Synch order %v", sc.Order)
	}

	cancel()
}
