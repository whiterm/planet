/*
Copyright 2016 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package leader

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/gravitational/trace"
	"github.com/jonboulle/clockwork"
	"github.com/sirupsen/logrus"
	"go.etcd.io/etcd/client"
)

var log = logrus.WithField(trace.Component, "leader")

// Client implements ETCD-backed leader election client
// that helps to elect new leaders for a given key and
// monitors the changes to the leaders
type Client struct {
	Config
	// closeC is the channel that indicates whether the client
	// should stop internal processes and exit
	closeC chan struct{}
	closed uint32
	// pauseC is the channel that control whether the client
	// should resign as a leader.
	// The handler will pause the client to make it resign and allow another peer to elect itself
	pauseC chan bool
	// voterC controls the voting participation.
	// The channel is serviced in an internal process
	// that either stops or restarts the election step when the client attempts to aquire
	// a lease on the leader key.
	// The send is not blocking
	voterC chan bool
	once   sync.Once
	wg     sync.WaitGroup

	// updateLeaseC receives lease updates.
	// Used in tests
	updateLeaseC chan *client.Response
}

// NewClient returns a new instance of leader election client
func NewClient(cfg Config) (*Client, error) {
	if err := cfg.checkAndSetDefaults(); err != nil {
		return nil, trace.Wrap(err)
	}
	return &Client{
		Config: cfg,
		closeC: make(chan struct{}),
		pauseC: make(chan bool),
		voterC: make(chan bool),
	}, nil
}

func (r *Config) checkAndSetDefaults() error {
	if r.Client == nil {
		return trace.BadParameter("Client is required")
	}
	if r.clock == nil {
		r.clock = clockwork.NewRealClock()
	}
	return nil
}

// Config sets leader election configuration options
type Config struct {
	// Client is the ETCD client to use
	Client client.Client
	clock  clockwork.Clock
}

// CallbackFn specifies callback that is called by AddWatchCallback
// whenever leader changes
type CallbackFn func(ctx context.Context, key, prevValue, newValue string)

// AddWatchCallback adds the given callback to be invoked when changes are
// made to the specified key's value. The callback is called with new and
// previous values for the key. In the first call, both values are the same
// and reflect the value of the key at that moment
func (l *Client) AddWatchCallback(key string, fn CallbackFn) {
	ctx, cancel := context.WithCancel(context.Background())
	valuesC := make(chan string)
	l.wg.Add(3)
	go func() {
		<-l.closeC
		cancel()
		l.wg.Done()
	}()
	go func() {
		defer l.wg.Done()
		var prev string
		for {
			select {
			case val := <-valuesC:
				fn(ctx, key, prev, val)
				prev = val
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		l.watchLoop(ctx, key, valuesC)
		l.wg.Done()
	}()
}

type ActionCallbackFn func(a Action)

func (l *Client) AddRecursiveWatchCallback(key string, fn ActionCallbackFn) {
	ctx, cancel := context.WithCancel(context.Background())
	valuesC := make(chan Action)
	go func() {
		for {
			select {
			case <-l.closeC:
				cancel()
				return
			case val := <-valuesC:
				fn(val)
			}
		}
	}()
	logger := log.WithField("key", key)
	logger.WithField("peers", l.Client.Endpoints()).Info("Setting up watch.")
	l.wg.Add(1)
	go l.watchRecursiveLoop(ctx, key, valuesC, logger)
}

// AddWatch starts watching the key for changes and sending them
// to the valuesC until the client is stopped.
func (l *Client) AddWatch(key string, valuesC chan string) {
	ctx, cancel := context.WithCancel(context.Background())
	l.wg.Add(2)
	go func() {
		<-l.closeC
		cancel()
		l.wg.Done()
	}()
	go func() {
		l.watchLoop(ctx, key, valuesC)
		l.wg.Done()
	}()
}

// AddVoter starts a goroutine that attempts to set the specified key to
// to the given value with the time-to-live value specified with term.
// The time-to-live value cannot be less than a second.
// After successfully setting the key, it attempts to renew the lease for the specified
// term indefinitely.
// The method is idempotent and does nothing if invoked multiple times
func (l *Client) AddVoter(ctx context.Context, key, value string, term time.Duration) {
	l.once.Do(func() {
		l.startVoterLoop(key, value, term, true)
	})
	select {
	case l.voterC <- true:
	case <-ctx.Done():
	case <-l.closeC:
	}
}

func (l *Client) LeaseLoop(ctx context.Context, key, value string, ttl time.Duration) {
	logger := log.WithFields(logrus.Fields{
		"key":   key,
		"value": value,
		"ttl":   ttl,
	})
	ticker := time.NewTicker(ttl / 5)
	defer func() {
		ticker.Stop()
	}()
	err := l.elect(ctx, key, value, ttl, logger)
	if err != nil {
		logger.WithError(err).Warn("Failed to run election term.")
	}
	for {
		select {
		case <-l.pauseC:
			logger.Info("Step down.")
			select {
			case <-ticker.C:
				logger.Info("Resume election participation.")
			case <-l.closeC:
				return
			}
		case <-l.closeC:
			logger.Info("Client is closing.")
			return
		case <-ctx.Done():
			logger.Info("LeaseLoop is closed.")
			return
		case <-ticker.C:
			err := l.elect(ctx, key, value, ttl, logger)
			if err != nil {
				logger.WithError(err).Warn("Failed to run election term.")
			}
		}
	}
}

// RemoveVoter stops the voting loop.
func (l *Client) RemoveVoter(ctx context.Context, key, value string, term time.Duration) {
	l.once.Do(func() {
		l.startVoterLoop(key, value, term, false)
	})
	select {
	case l.voterC <- false:
	case <-ctx.Done():
	case <-l.closeC:
	}
}

// StepDown makes this participant to pause his attempts to re-elect itself thus giving up its leadership
func (l *Client) StepDown(ctx context.Context) {
	select {
	case l.pauseC <- true:
	case <-ctx.Done():
	}
}

// Close stops current operations and releases resources
func (l *Client) Close() error {
	// already closed
	if !atomic.CompareAndSwapUint32(&l.closed, 0, 1) {
		return nil
	}
	close(l.closeC)
	l.wg.Wait()
	return nil
}

func (l *Client) startVoterLoop(key, value string, term time.Duration, enabled bool) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-l.closeC
		cancel()
	}()
	go l.voterLoop(ctx, key, value, term, enabled)
}

func (l *Client) watchRecursiveLoop(ctx context.Context, key string, valuesC chan Action, logger logrus.FieldLogger) {
	defer l.wg.Done()
	boff := newBackoff()
	// maxFailedSteps sets the limit on the number of failed attempts before the watch
	// is forcibly reset
	const maxFailedSteps = 10
	var (
		api     = client.NewKeysAPI(l.Client)
		watcher client.Watcher
	)
	var err error
	for {
		select {
		case <-time.After(boff.NextBackOff()):
		case <-l.closeC:
			logger.Info("Watch loop closing.")
			return
		}
		if watcher == nil {
			watcher, err = l.getRecursiveWatch(ctx, api, key, valuesC, logger)
			if err != nil {
				if IsContextError(err) {
					logger.Info("Context expired, watch loop closing.")
					return
				} else if IsWatchExpired(err) {
					// The watcher has expired, reset it so it's recreated on the
					// next loop cycle.
					logger.Warn("Watch has expired, resetting watch index.")
					watcher = nil
				} else {
					logger.WithError(err).Warn("Failed to create watch at latest index.")
					boff.inc()
					if boff.count() > maxFailedSteps {
						logger.Info("Reset watcher at latest index.")
						watcher = nil
						boff.Reset()
					}
				}
				continue
			}
			//Successful return means the current value has already been sent to receiver
		}
		for {
			resp, err := watcher.Next(ctx)
			if err != nil {
				if IsContextError(err) {
					return
				}
				logger.WithError(err).Warn("Failed to retrieve event from watcher.")
				watcher = nil
				break
			}
			boff.Reset()
			actions := respToActions(resp)
			for _, action := range actions {
				select {
				case valuesC <- action:
				case <-l.closeC:
					logger.Info("Watcher is closing.")
					return
				}
			}
		}
	}
}

func (l *Client) watchLoop(ctx context.Context, key string, valuesC chan string) {
	logger := log.WithFields(logrus.Fields{
		"key":   key,
		"peers": l.Client.Endpoints(),
	})
	logger.Info("Setting up watch.")
	boff := newBackoff()
	// maxFailedSteps sets the limit on the number of failed attempts before the watch
	// is forcibly reset
	const maxFailedSteps = 10
	var (
		api     = client.NewKeysAPI(l.Client)
		watcher client.Watcher
	)
	var err error
	for {
		select {
		case <-time.After(boff.NextBackOff()):
		case <-l.closeC:
			logger.Info("Watch loop closing.")
			return
		}
		if watcher == nil {
			watcher, err = l.getWatchAtLatestIndex(ctx, api, key, valuesC, logger)
			if err != nil {
				if IsContextError(err) {
					logger.Info("Context expired, watch loop closing.")
					return
				} else if IsWatchExpired(err) {
					// The watcher has expired, reset it so it's recreated on the
					// next loop cycle.
					logger.Warn("Watch has expired, resetting watch index.")
					watcher = nil
				} else {
					logger.WithError(err).Warn("Failed to create watch at latest index.")
					boff.inc()
					if boff.count() > maxFailedSteps {
						logger.Info("Reset watcher at latest index.")
						watcher = nil
						boff.Reset()
					}
				}
				continue
			}
			// Successful return means the current value has already been sent to receiver
		}
		for {
			resp, err := watcher.Next(ctx)
			if err != nil {
				if IsContextError(err) {
					return
				}
				logger.WithError(err).Warn("Failed to retrieve event from watcher.")
				watcher = nil
				break
			}
			boff.Reset()
			select {
			case valuesC <- resp.Node.Value:
			case <-l.closeC:
				logger.Info("Watcher is closing.")
				return
			}
		}
	}
}

// voterLoop is a process that attempts to set the specified key to
// to the given value with the time-to-live value specified with term.
// The time-to-live value cannot be less than a second.
// After successfully setting the key, it attempts to renew the lease for the specified
// term indefinitely.
// The specified context is bound to the underlying close chan and will expire when the client is closed
func (l *Client) voterLoop(ctx context.Context, key, value string, term time.Duration, enabled bool) {
	logger := log.WithFields(logrus.Fields{
		"key":   key,
		"value": value,
		"term":  term,
	})
	var ticker clockwork.Ticker
	var tickerC <-chan time.Time
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()
	if enabled {
		err := l.elect(ctx, key, value, term, logger)
		if err != nil {
			logger.WithError(err).Warn("Failed to run election term.")
		}
		ticker = l.clock.NewTicker(term / 5)
		tickerC = ticker.Chan()
	}
	for {
		select {
		case <-l.pauseC:
			logger.Info("Step down.")
			select {
			case <-l.clock.After(term * 2):
				logger.Info("Resume election participation.")
			case <-l.closeC:
				return
			}

		case <-tickerC:
			err := l.elect(ctx, key, value, term, logger)
			if err != nil {
				logger.WithError(err).Warn("Failed to run election term.")
			}

		case enabled := <-l.voterC:
			if !enabled {
				logger.Info("Pause election participation.")
				if ticker != nil {
					ticker.Stop()
				}
				ticker = nil
				tickerC = nil
				continue
			}
			if tickerC == nil {
				ticker = l.clock.NewTicker(term / 5)
				tickerC = ticker.Chan()
			}

		case <-l.closeC:
			logger.Info("Voter is closing.")
			return
		}
	}
}

func respToActions(resp *client.Response) []Action {
	if resp == nil || resp.Node == nil {
		return nil
	}
	t := toActionType(resp.Action)
	// this is not a update
	if !resp.Node.Dir && t == ActionTypeUpdate &&
		resp.PrevNode != nil &&
		resp.PrevNode.Key == resp.Node.Key &&
		resp.PrevNode.Value == resp.Node.Value {
		return nil
	}
	return nodeToActions(t, resp.Node)
}

func nodeToActions(t ActionType, node *client.Node) []Action {
	if node == nil {
		return nil
	}
	result := make([]Action, 0)
	if !node.Dir {
		result = append(result, Action{
			Type:  t,
			Key:   node.Key,
			Value: node.Value,
		})
		return result
	}
	for _, n := range node.Nodes {
		result = append(result, nodeToActions(t, n)...)
	}
	return result
}

func toActionType(val string) ActionType {
	switch val {
	case "get", "set", "create":
		return ActionTypeCreate
	case "compareAndSwap", "update":
		return ActionTypeUpdate
	case "compareAndDelete", "expire", "delete":
		return ActionTypeDelete
	}
	return ActionType(val)
}

func (l *Client) getRecursiveWatch(ctx context.Context, api client.KeysAPI, key string, valuesC chan Action, logger logrus.FieldLogger) (client.Watcher, error) {
	logger = logger.WithField("key", key)
	logger.Info("Recreating watch at the latest index.")
	resp, err := api.Get(ctx, key, &client.GetOptions{
		Recursive: true,
	})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	// After reestablishing the watch, always send the value we got to the client.
	if resp.Node != nil {
		logger.WithFields(logrus.Fields{
			"value": resp.Node.Value,
			"index": resp.Index,
		}).Info("Got current value.")
		actions := respToActions(resp)
		for _, action := range actions {
			select {
			case valuesC <- action:
			case <-l.closeC:
				return nil, trace.LimitExceeded("client closed")
			}
		}

	}
	// The watcher that will be receiving events after the value we got above.
	watcher := api.Watcher(key, &client.WatcherOptions{
		Recursive: true,
		// Response.Index corresponds to X-Etcd-Index response header field
		// and is the recommended starting point after a history miss of over
		// 1000 events
		AfterIndex: resp.Index,
	})
	return watcher, nil
}

func (l *Client) getWatchAtLatestIndex(ctx context.Context, api client.KeysAPI, key string, valuesC chan string, logger logrus.FieldLogger) (client.Watcher, error) {
	logger = logger.WithField("key", key)
	logger.Info("Recreating watch at the latest index.")
	resp, err := api.Get(ctx, key, nil)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	// After reestablishing the watch, always send the value we got to the client.
	if resp.Node != nil {
		logger.WithFields(logrus.Fields{
			"value": resp.Node.Value,
			"index": resp.Index,
		}).Info("Got current value.")
		select {
		case valuesC <- resp.Node.Value:
		case <-l.closeC:
			return nil, trace.LimitExceeded("client closed")
		}
	}
	// The watcher that will be receiving events after the value we got above.
	watcher := api.Watcher(key, &client.WatcherOptions{
		// Response.Index corresponds to X-Etcd-Index response header field
		// and is the recommended starting point after a history miss of over
		// 1000 events
		AfterIndex: resp.Index,
	})
	return watcher, nil
}

// elect is taken from: https://github.com/kubernetes/contrib/blob/master/pod-master/podmaster.go
// this is a slightly modified version though, that does not return the result
// instead we rely on watchers
func (l *Client) elect(ctx context.Context, key, value string, term time.Duration, logger logrus.FieldLogger) error {
	api := client.NewKeysAPI(l.Client)
	resp, err := api.Get(ctx, key, nil)
	if err != nil {
		if !IsNotFound(err) {
			return trace.Wrap(err)
		}
		// try to grab the lock for the given term
		node, err := api.Set(ctx, key, value, &client.SetOptions{
			TTL:       term,
			PrevExist: client.PrevNoExist,
		})
		if err != nil {
			return trace.Wrap(err)
		}
		l.updateLease(node)
		logger.Info("Acquired lease.")
		return nil
	}
	if resp.Node.Value != value {
		return nil
	}
	if resp.Node.Expiration.Sub(l.clock.Now().UTC()) > time.Duration(term/2) {
		return nil
	}
	// extend the lease before the current expries
	node, err := api.Set(ctx, key, value, &client.SetOptions{
		TTL:       term,
		PrevValue: value,
		PrevIndex: resp.Node.ModifiedIndex,
	})
	if err != nil {
		return trace.Wrap(err)
	}
	l.updateLease(node)
	logger.Debug("Extended lease.")
	return nil
}

func (l *Client) updateLease(node *client.Response) {
	select {
	case l.updateLeaseC <- node:
	case <-l.closeC:
	default:
	}
}

func newBackoff() *countedBackoff {
	return &countedBackoff{
		b: backoff.NewExponentialBackOff(),
	}
}

func (r *countedBackoff) Reset() {
	r.steps = 0
	r.b.Reset()
}

func (r *countedBackoff) NextBackOff() time.Duration {
	return r.b.NextBackOff()
}

func (r *countedBackoff) inc() {
	r.steps += 1
}

func (r *countedBackoff) count() int {
	return r.steps
}

type countedBackoff struct {
	b     *backoff.ExponentialBackOff
	steps int
}
