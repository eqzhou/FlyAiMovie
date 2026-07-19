package production

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Worker struct {
	Service  *Service
	Interval time.Duration
	once     sync.Once
	stopOnce sync.Once
	stop     chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func (w *Worker) Start() {
	w.once.Do(func() {
		w.stop = make(chan struct{})
		w.ctx, w.cancel = context.WithCancel(context.Background())
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.loop()
		}()
	})
}

func (w *Worker) Stop() {
	if w.stop == nil {
		return
	}
	w.stopOnce.Do(func() {
		if w.cancel != nil {
			w.cancel()
		}
		close(w.stop)
	})
	w.wg.Wait()
}

func (w *Worker) loop() {
	interval := w.Interval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	owner := "production-" + uuid.NewString()
	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			if _, err := w.Service.ProcessAvailable(w.ctx, owner, 5); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("process production runs: %v", err)
			}
		}
	}
}
