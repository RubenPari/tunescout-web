// Package jobs esegue in background le operazioni lunghe (sync, discover,
// prune) e ne trasmette il progresso agli abbonati (SSE).
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rubenpari/tunescout-web/internal/store"
)

// ErrBusy indica che l'utente ha già un job in esecuzione.
var ErrBusy = errors.New("hai già un'operazione in corso: attendi che finisca")

const (
	jobTimeout = 10 * time.Minute
	keepLive   = 5 * time.Minute // per quanto restano in memoria i log di un job finito
)

// Tipi di job.
const (
	KindSync     = "sync"
	KindDiscover = "discover"
	KindPrune    = "prune"
)

// Tipi di evento trasmessi agli abbonati.
const (
	EventLog  = "log"  // riga di progresso
	EventDone = "done" // job terminato, con lo stato finale
)

type Event struct {
	Type    string     `json:"type"` // EventLog o EventDone
	Message string     `json:"message,omitempty"`
	Job     *store.Job `json:"job,omitempty"`
}

// UserError separa il messaggio salvato nel job e mostrato all'utente (Msg)
// dalla causa originale (Err), che il runner registra nei log.
type UserError struct {
	Msg string
	Err error
}

func (e *UserError) Error() string { return e.Msg }
func (e *UserError) Unwrap() error { return e.Err }

// Func è il lavoro di un job: log invia righe di progresso, il valore
// restituito viene salvato come risultato JSON.
type Func func(ctx context.Context, log func(string, ...any)) (any, error)

type live struct {
	mu     sync.Mutex
	logs   []string
	final  *store.Job
	subs   map[chan Event]struct{}
	userID int64
}

type Runner struct {
	DB *store.DB

	sem    chan struct{}
	mu     sync.Mutex
	active map[int64]string // utente → job in corso
	jobs   map[string]*live
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

func NewRunner(db *store.DB, maxConcurrent int) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{
		DB: db, sem: make(chan struct{}, maxConcurrent),
		active: map[int64]string{}, jobs: map[string]*live{},
		ctx: ctx, cancel: cancel,
	}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Start registra il job e lo avvia in background.
// onFail, se non nil, viene chiamata quando il job fallisce.
func (r *Runner) Start(userID int64, kind string, params any, fn Func, onFail func()) (*store.Job, error) {
	p, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	if _, busy := r.active[userID]; busy {
		r.mu.Unlock()
		return nil, ErrBusy
	}
	j := &store.Job{ID: newID(), UserID: userID, Kind: kind, Status: store.JobRunning, Params: p}
	r.active[userID] = j.ID
	r.mu.Unlock()

	if err := r.DB.CreateJob(r.ctx, j); err != nil {
		r.mu.Lock()
		delete(r.active, userID)
		r.mu.Unlock()
		return nil, err
	}
	l := &live{subs: map[chan Event]struct{}{}, userID: userID}
	r.mu.Lock()
	r.jobs[j.ID] = l
	r.mu.Unlock()

	r.wg.Add(1)
	go r.run(j, l, fn, onFail)
	return j, nil
}

func (r *Runner) run(j *store.Job, l *live, fn Func, onFail func()) {
	defer r.wg.Done()
	logf := func(format string, a ...any) { l.publish(Event{Type: EventLog, Message: fmt.Sprintf(format, a...)}) }

	select {
	case r.sem <- struct{}{}:
	default:
		logf("In coda: altre operazioni in corso sul server…")
		select {
		case r.sem <- struct{}{}:
		case <-r.ctx.Done():
		}
	}

	var res any
	err := r.ctx.Err()
	if err == nil {
		ctx, cancel := context.WithTimeout(r.ctx, jobTimeout)
		res, err = safeRun(ctx, fn, logf)
		cancel()
		<-r.sem
	}

	status, errMsg := store.JobDone, ""
	var out []byte
	if err == nil {
		out, err = json.Marshal(res)
	}
	if err != nil {
		status, errMsg = store.JobFailed, err.Error()
		if errors.Is(err, context.DeadlineExceeded) {
			errMsg = "tempo massimo superato"
		}
		out = nil
		cause := err
		if ue, ok := errors.AsType[*UserError](err); ok && ue.Err != nil {
			cause = ue.Err
		}
		slog.Warn("job fallito", "job", j.ID, "kind", j.Kind, "user", j.UserID, "err", cause)
		if onFail != nil {
			onFail()
		}
	}
	if err := r.DB.FinishJob(context.WithoutCancel(r.ctx), j.ID, status, out, errMsg); err != nil {
		slog.Error("salvataggio job", "job", j.ID, "err", err)
	}
	now := time.Now()
	j.Status, j.Result, j.Error, j.FinishedAt = status, out, errMsg, &now

	r.mu.Lock()
	delete(r.active, j.UserID)
	r.mu.Unlock()
	l.finish(j)

	time.AfterFunc(keepLive, func() {
		r.mu.Lock()
		delete(r.jobs, j.ID)
		r.mu.Unlock()
	})
}

func safeRun(ctx context.Context, fn Func, logf func(string, ...any)) (res any, err error) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("panic nel job", "panic", p)
			err = errors.New("errore interno")
		}
	}()
	return fn(ctx, logf)
}

// Active restituisce il job in corso dell'utente, se c'è.
func (r *Runner) Active(userID int64) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.active[userID]
	return id, ok
}

// Subscribe restituisce i log già emessi e un canale per i successivi.
// ok è false se il job non è più in memoria (usare lo stato salvato su DB).
// Il canale viene chiuso dopo l'evento EventDone.
func (r *Runner) Subscribe(userID int64, jobID string) (past []string, ch <-chan Event, cancel func(), ok bool) {
	r.mu.Lock()
	l, ok := r.jobs[jobID]
	r.mu.Unlock()
	if !ok || l.userID != userID {
		return nil, nil, nil, false
	}
	c := make(chan Event, 64)
	l.mu.Lock()
	defer l.mu.Unlock()
	past = append([]string(nil), l.logs...)
	if l.final != nil {
		c <- Event{Type: EventDone, Job: l.final}
		close(c)
		return past, c, func() {}, true
	}
	l.subs[c] = struct{}{}
	return past, c, func() {
		l.mu.Lock()
		if _, ok := l.subs[c]; ok {
			delete(l.subs, c)
			close(c)
		}
		l.mu.Unlock()
	}, true
}

func (l *live) publish(e Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, e.Message)
	for c := range l.subs {
		select {
		case c <- e:
		default: // abbonato lento: perde la riga, non blocca il job
		}
	}
}

func (l *live) finish(j *store.Job) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.final = j
	for c := range l.subs {
		select {
		case c <- Event{Type: EventDone, Job: j}:
		default:
		}
		close(c)
		delete(l.subs, c)
	}
}

// Shutdown annulla i job in corso e attende che terminino.
func (r *Runner) Shutdown(ctx context.Context) error {
	r.cancel()
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
