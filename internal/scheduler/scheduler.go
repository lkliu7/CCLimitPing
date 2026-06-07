// Package scheduler runs the watch loop: for each provider it sleeps until the
// 5h window resets, then triggers a minimal ping to start the next window,
// keeping windows back-to-back. It respects the weekly limit and never lets a
// transient error kill the loop.
package scheduler

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/wavever/CCLimitPing/internal/config"
	"github.com/wavever/CCLimitPing/internal/notify"
	"github.com/wavever/CCLimitPing/internal/provider"
	"github.com/wavever/CCLimitPing/internal/usage"
)

const (
	postPingGrace  = 15 * time.Second // wait after a ping before re-reading usage
	minBackoff     = 30 * time.Second
	maxBackoff     = 10 * time.Minute
	defaultWindow  = 5 * time.Hour // fallback when the API omits the window length
	maxSleepChunk  = 30 * time.Minute
	readTimeout    = 30 * time.Second
	triggerTimeout = 3 * time.Minute
	activeTaskPoll = time.Minute
)

// Target pairs a provider with its scheduling options.
type Target struct {
	Provider   provider.Provider
	AlignStart time.Time // zero = ping as soon as the window is free
}

// Scheduler drives the watch loops.
type Scheduler struct {
	cfg     config.Config
	targets []Target
	dryRun  bool
	log     *log.Logger
}

func New(cfg config.Config, targets []Target, dryRun bool, logger *log.Logger) *Scheduler {
	return &Scheduler{cfg: cfg, targets: targets, dryRun: dryRun, log: logger}
}

// Run starts one loop per target and blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	done := make(chan struct{}, len(s.targets))
	for _, t := range s.targets {
		go func(t Target) {
			s.runTarget(ctx, t)
			done <- struct{}{}
		}(t)
	}
	for range s.targets {
		<-done
	}
}

func (s *Scheduler) runTarget(ctx context.Context, t Target) {
	name := t.Provider.Name()
	backoff := minBackoff
	aligned := t.AlignStart.IsZero() // whether the align gate has been passed
	var lastPingAt time.Time

	for {
		if ctx.Err() != nil {
			return
		}

		rctx, cancel := context.WithTimeout(ctx, readTimeout)
		u, err := t.Provider.ReadUsage(rctx)
		cancel()
		if err != nil {
			s.log.Printf("[%s] read usage failed: %v (retry in %s)", name, err, backoff)
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = minBackoff

		// Respect the weekly limit: if exhausted (and no usable credits), wait
		// for the weekly window to reset instead of pinging.
		if s.weeklyExhausted(u) {
			wait := u.Weekly.Remaining() + s.cfg.ResetBuffer.Duration
			if wait <= 0 {
				wait = time.Minute
			}
			s.log.Printf("[%s] weekly limit exhausted (%.0f%%); sleeping %s until weekly reset",
				name, u.Weekly.UsedPercent, wait.Round(time.Second))
			s.notify(name+": weekly limit reached", "Skipping pings until weekly reset")
			if !sleepCtx(ctx, wait) {
				return
			}
			continue
		}

		// If the 5h window is still running, wait until it resets, then ping.
		if u.FiveHour.Active() {
			wait := u.FiveHour.Remaining() + s.cfg.ResetBuffer.Duration
			s.log.Printf("[%s] 5h window active (%.0f%%), next ping at %s (in %s)",
				name, u.FiveHour.UsedPercent,
				u.FiveHour.ResetsAt.Local().Format("15:04:05"), wait.Round(time.Second))
			if !sleepCtx(ctx, wait) {
				return
			}
			continue
		}

		// Window is free. Guard against double-pinging if our last ping isn't
		// reflected by the endpoint yet.
		if !lastPingAt.IsZero() {
			est := lastPingAt.Add(windowLen(u.FiveHour))
			if time.Now().Before(est) {
				wait := time.Until(est) + s.cfg.ResetBuffer.Duration
				s.log.Printf("[%s] recent ping not yet visible; waiting %s", name, wait.Round(time.Second))
				if !sleepCtx(ctx, wait) {
					return
				}
				continue
			}
		}

		// Honor the first-window alignment anchor, once.
		if !aligned {
			if d := time.Until(t.AlignStart); d > 0 {
				s.log.Printf("[%s] waiting for align_start %s (in %s)",
					name, t.AlignStart.Local().Format("15:04:05"), d.Round(time.Second))
				if !sleepCtx(ctx, d) {
					return
				}
			}
			aligned = true
		}

		if desc, active, err := activeProviderTask(ctx, t.Provider); err != nil {
			s.log.Printf("[%s] active task check failed: %v; pinging anyway", name, err)
		} else if active {
			s.log.Printf("[%s] window reset but %s is running; waiting %s for it to start the next window",
				name, desc, activeTaskPoll.Round(time.Second))
			if !sleepCtx(ctx, activeTaskPoll) {
				return
			}
			continue
		}

		// Trigger the window.
		if !s.dryRun {
			s.log.Printf("[%s] window reset — triggering ping now…", name)
		}
		tctx, tcancel := context.WithTimeout(ctx, triggerTimeout)
		res, err := t.Provider.Trigger(tctx, s.dryRun)
		tcancel()
		if s.dryRun {
			if err != nil {
				s.log.Printf("[%s] dry-run ping failed: %v (retry in %s)", name, err, backoff)
				if !sleepCtx(ctx, backoff) {
					return
				}
				backoff = nextBackoff(backoff)
				continue
			}
			s.log.Printf("[%s] DRY-RUN would ping now: %s", name, res.Command)
			// In dry-run we can't actually start a window, so estimate the next
			// cycle from the configured window length to keep the loop sane.
			lastPingAt = time.Now()
			continue
		}
		if err != nil {
			s.log.Printf("[%s] ping failed: %v (retry in %s)", name, err, backoff)
			s.notify(name+": ping failed", err.Error())
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		lastPingAt = time.Now()
		s.log.Printf("[%s] ping sent, new window started%s", name, triggerCost(res))
		s.notify(name+": window started", "New 5h window"+triggerCost(res))

		if !sleepCtx(ctx, postPingGrace) {
			return
		}
	}
}

func (s *Scheduler) weeklyExhausted(u *usage.Usage) bool {
	if u.CreditsUsable() {
		return false
	}
	return u.Weekly.UsedPercent/100 >= s.cfg.WeeklyThreshold
}

func activeProviderTask(ctx context.Context, p provider.Provider) (string, bool, error) {
	detector, ok := p.(provider.ActiveTaskDetector)
	if !ok {
		return "", false, nil
	}
	return detector.ActiveTask(ctx)
}

func (s *Scheduler) notify(title, msg string) {
	if s.cfg.Notify {
		notify.Notify(title, msg)
	}
}

// triggerCost renders the token/cost tail for logs, e.g.
// " — 32934 tok (in 32792 / out 142), $0.0110".
func triggerCost(res *provider.TriggerResult) string {
	if res == nil || !res.HasUsage {
		return ""
	}
	s := fmt.Sprintf(" — %d tok (in %d / out %d)", res.TotalTokens, res.InputTokens, res.OutputTokens)
	if res.CostUSD > 0 {
		s += fmt.Sprintf(", $%.4f", res.CostUSD)
	}
	return s
}

func windowLen(w usage.Window) time.Duration {
	if w.WindowSeconds > 0 {
		return time.Duration(w.WindowSeconds) * time.Second
	}
	return defaultWindow
}

func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > maxBackoff {
		return maxBackoff
	}
	return d
}

// sleepCtx sleeps for d (in chunks, so long sleeps stay responsive to
// cancellation) and reports false if the context was cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return true
		}
		chunk := remaining
		if chunk > maxSleepChunk {
			chunk = maxSleepChunk
		}
		t := time.NewTimer(chunk)
		select {
		case <-ctx.Done():
			t.Stop()
			return false
		case <-t.C:
		}
	}
}
