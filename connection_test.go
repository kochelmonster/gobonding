package gobonding_test

import (
	"context"
	"testing"

	"github.com/kochelmonster/gobonding"
)

func createConnManager(ctx context.Context) *gobonding.ConnManager {
	config := gobonding.Config{}
	return gobonding.NewConnMananger(ctx, &config)
}

func TestAllocAndFree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cm := createConnManager(ctx)

	chunk1 := cm.AllocChunk()
	chunk2 := cm.AllocChunk()
	chunk1.Size = 1
	chunk2.Size = 2
	cm.FreeChunk(chunk1)

	chunk3 := cm.AllocChunk()
	if chunk3.Size != 1 {
		t.Fatalf("Chunk not reused")
	}

	cm.FreeChunk(chunk3)
	cm.FreeChunk(chunk2)

	cancel()
}
