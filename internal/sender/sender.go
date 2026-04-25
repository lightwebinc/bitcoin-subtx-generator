// Package sender is the worker pool that generates and transmits frames.
//
// Concurrency model:
//   - One net.UDPConn per worker (Dial'ed once). Each worker owns its
//     encoding buffer and per-worker PRNG, so the hot path is lock-free.
//   - A central pacer (internal/rate) gates emission. Workers pull tokens
//     from a shared channel; backpressure is natural because Wait() blocks.
//   - Sequence numbers come from a shared atomic allocator (internal/seq).
//   - Subtree IDs are chosen deterministically from a read-only Pool.
package sender

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	common "github.com/lightwebinc/bitcoin-shard-common/frame"

	myframe "github.com/lightwebinc/bitcoin-subtx-generator/internal/frame"
	"github.com/lightwebinc/bitcoin-subtx-generator/internal/rate"
	"github.com/lightwebinc/bitcoin-subtx-generator/internal/seq"
	"github.com/lightwebinc/bitcoin-subtx-generator/internal/subtree"
	"github.com/lightwebinc/bitcoin-subtx-generator/internal/tx"
)

// Config tunes the sender.
type Config struct {
	Addr             string // target host:port
	FrameVersion     myframe.Version
	Workers          int
	PPS              int
	Duration         time.Duration // 0 = run until Count frames sent or ctx canceled
	Count            uint64        // 0 = unlimited
	PayloadSize      int
	SenderID         [16]byte
	LogInterval      time.Duration
	FlowResetPackets uint64        // reset SequenceID after this many packets (0 = disabled)
	FlowResetTime    time.Duration // reset SequenceID after this duration (0 = disabled)
}

// Runner ties together the pacer, seq allocator, subtree pool, and worker pool.
type Runner struct {
	cfg   Config
	pool  *subtree.Pool
	alloc *seq.Allocator

	sent          atomic.Uint64
	bytes         atomic.Uint64
	errors        atomic.Uint64
	sequenceID    atomic.Uint64
	flowResetCnt  atomic.Uint64
	flowResetTime time.Time
}

// New creates a Runner.
func New(cfg Config, pool *subtree.Pool, alloc *seq.Allocator) *Runner {
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.PayloadSize <= 0 {
		cfg.PayloadSize = 256
	}
	if cfg.LogInterval <= 0 {
		cfg.LogInterval = time.Second
	}
	r := &Runner{cfg: cfg, pool: pool, alloc: alloc}
	// Initialize with a random SequenceID
	var sidBuf [8]byte
	if _, err := cryptorand.Read(sidBuf[:]); err == nil {
		r.sequenceID.Store(binary.BigEndian.Uint64(sidBuf[:]))
	}
	r.flowResetTime = time.Now()
	return r
}

// Run blocks until ctx is canceled, Count is reached, or Duration elapses.
// Returns the number of frames transmitted.
func (r *Runner) Run(ctx context.Context) (uint64, error) {
	// Derive a run deadline if Duration is set.
	runCtx := ctx
	if r.cfg.Duration > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, r.cfg.Duration)
		defer cancel()
	}

	pacer := rate.New(r.cfg.PPS)
	defer pacer.Stop()

	tokens := make(chan struct{}, r.cfg.Workers*2)
	var wg sync.WaitGroup

	// Dispatcher goroutine: drives pacer, counts issued tokens against Count.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(tokens)
		for {
			if err := runCtx.Err(); err != nil {
				return
			}
			if r.cfg.Count > 0 && r.sent.Load() >= r.cfg.Count {
				return
			}
			if !pacer.Wait() {
				return
			}
			select {
			case tokens <- struct{}{}:
			case <-runCtx.Done():
				return
			}
		}
	}()

	// Workers.
	for i := 0; i < r.cfg.Workers; i++ {
		wg.Add(1)
		go r.worker(runCtx, i, tokens, &wg)
	}

	// Periodic logger.
	logDone := make(chan struct{})
	go r.logger(runCtx, logDone)

	wg.Wait()
	close(logDone)
	return r.sent.Load(), nil
}

func (r *Runner) worker(ctx context.Context, id int, tokens <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	conn, err := net.Dial("udp", r.cfg.Addr)
	if err != nil {
		log.Printf("worker %d: dial %s: %v", id, r.cfg.Addr, err)
		return
	}
	defer func() { _ = conn.Close() }()

	// Per-worker PRNG seed.
	var seed [32]byte
	if _, err := cryptorand.Read(seed[:]); err != nil {
		log.Printf("worker %d: seed: %v", id, err)
		return
	}
	seed[0] ^= byte(id)

	builder := tx.New(seed)

	hdrSize := myframe.HeaderSize(r.cfg.FrameVersion)
	buf := make([]byte, hdrSize+r.cfg.PayloadSize)
	payload := make([]byte, r.cfg.PayloadSize)

	f := &common.Frame{SenderID: r.cfg.SenderID, SequenceID: r.sequenceID.Load()}

	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-tokens:
			if !ok {
				return
			}
		}

		if r.cfg.Count > 0 && r.sent.Load() >= r.cfg.Count {
			return
		}

		// Random TxID.
		if _, err := cryptorand.Read(f.TxID[:]); err != nil {
			r.errors.Add(1)
			continue
		}

		// Random payload.
		payload = builder.Build(payload[:0:cap(payload)], r.cfg.PayloadSize)
		f.Payload = payload

		// Sequence number + subtree.
		s := r.alloc.Next()
		f.SeqNum = s

		// Update SequenceID with flow reset logic
		f.SequenceID = r.sequenceID.Load()
		if r.cfg.FlowResetPackets > 0 {
			cnt := r.flowResetCnt.Add(1)
			if cnt >= r.cfg.FlowResetPackets {
				r.resetSequenceID()
			}
		}
		if r.cfg.FlowResetTime > 0 {
			if time.Since(r.flowResetTime) >= r.cfg.FlowResetTime {
				r.resetSequenceID()
			}
		}

		// SubtreeID chosen by txid high bits so listeners filtering on a
		// single subtree see a predictable fraction of traffic.
		sel := binary.BigEndian.Uint64(f.TxID[:8])
		f.SubtreeID = r.pool.Pick(sel)

		n, err := myframe.Encode(r.cfg.FrameVersion, f, buf)
		if err != nil {
			r.errors.Add(1)
			continue
		}
		if _, err := conn.Write(buf[:n]); err != nil {
			r.errors.Add(1)
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		r.sent.Add(1)
		r.bytes.Add(uint64(n))
	}
}

func (r *Runner) logger(ctx context.Context, done <-chan struct{}) {
	t := time.NewTicker(r.cfg.LogInterval)
	defer t.Stop()
	var lastSent, lastBytes uint64
	lastTime := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case now := <-t.C:
			s := r.sent.Load()
			b := r.bytes.Load()
			dt := now.Sub(lastTime).Seconds()
			if dt <= 0 {
				continue
			}
			pps := float64(s-lastSent) / dt
			mbps := float64(b-lastBytes) * 8 / dt / 1e6
			fmt.Fprintf(os.Stderr, "[subtx-gen] sent=%d pps=%.0f mbps=%.2f errs=%d\n",
				s, pps, mbps, r.errors.Load())
			lastSent = s
			lastBytes = b
			lastTime = now
		}
	}
}

// Sent returns the total frames successfully transmitted so far.
func (r *Runner) Sent() uint64 { return r.sent.Load() }

// Errors returns the total send errors observed.
func (r *Runner) Errors() uint64 { return r.errors.Load() }

// resetSequenceID generates a new random SequenceID and resets the flow reset counters.
func (r *Runner) resetSequenceID() {
	var sidBuf [8]byte
	if _, err := cryptorand.Read(sidBuf[:]); err == nil {
		r.sequenceID.Store(binary.BigEndian.Uint64(sidBuf[:]))
	}
	r.flowResetCnt.Store(0)
	r.flowResetTime = time.Now()
}
