package workers

import (
	"context"
	"sync"
	"time"
)

type Manager interface {
	Spawn(ctx context.Context, wg *sync.WaitGroup, sendTicker *time.Ticker)
	CloseOne()
	CloseAll(cancel context.CancelFunc, wg *sync.WaitGroup)
}
