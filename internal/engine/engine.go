// Package engine watches for configured devices and runs the one-way upload + verify jobs,
// using rclone as a library. Nothing in Drive is ever deleted or overwritten.
package engine

import (
	"bytes"
	"log"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rclone/rclone/backend/drive"
	"github.com/rclone/rclone/backend/local"
	rfs "github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/filter"
	"github.com/rclone/rclone/fs/operations"
	rsync "github.com/rclone/rclone/fs/sync"
	"golang.org/x/oauth2"

	"github.com/cknm-cmd/cardhaul/internal/config"
	"github.com/cknm-cmd/cardhaul/internal/gauth"
	"github.com/cknm-cmd/cardhaul/internal/volumes"
)

// DriveRoot is the folder CardHaul creates in the user's Drive; each device gets a subfolder.
const DriveRoot = "CardHaul"

type Phase string

const (
	PhaseIdle      Phase = "idle"
	PhaseScanning  Phase = "scanning"
	PhaseUploading Phase = "uploading"
	PhaseVerifying Phase = "verifying"
	PhaseDone      Phase = "done"
	PhaseProblem   Phase = "problem"
)

type FileProgress struct {
	Name    string `json:"name"`
	Percent int    `json:"percent"`
}

// Snapshot is everything the UI shows. It's sent to the frontend on every change.
type Snapshot struct {
	Phase        Phase          `json:"phase"`
	VerifyMode   string         `json:"verifyMode"` // mode the current check is actually using
	Paused       bool           `json:"paused"`
	SignedIn     bool           `json:"signedIn"`
	Device       string         `json:"device"`
	Dest         string         `json:"dest"`
	BytesDone    int64          `json:"bytesDone"`
	BytesTotal   int64          `json:"bytesTotal"`
	FilesDone    int            `json:"filesDone"`
	FilesTotal   int            `json:"filesTotal"`
	Speed        float64        `json:"speed"` // bytes/s
	ETA          float64        `json:"eta"`   // seconds, -1 when unknown
	Elapsed      float64        `json:"elapsed"`
	Current      []FileProgress `json:"current"`
	Message      string         `json:"message"`
	ProblemFiles []string       `json:"problemFiles"`
	Connected    []string       `json:"connected"` // configured devices currently plugged in
	Cancelled    []string       `json:"cancelled"` // devices skipped until unplugged
	Last         *config.Run    `json:"last"`
}

type Engine struct {
	store     *config.Store
	configDir string
	onChange  func(Snapshot)
	notify    func(title, body string)

	mu        sync.Mutex
	snap      Snapshot
	busy      bool
	cancelJob context.CancelFunc
	cancelled map[string]string // device label -> connection ID it was cancelled for
	// checked remembers devices already handled on their current connection, so the 2-second
	// device poll doesn't re-check (and re-query Drive) the same plugged-in device forever.
	checked map[string]string
	kick      chan struct{}
}

func New(store *config.Store, configDir string, onChange func(Snapshot), notify func(title, body string)) *Engine {
	ctx := context.Background()
	rfs.GetConfig(ctx).LogLevel = rfs.LogLevelNotice
	e := &Engine{
		store:     store,
		configDir: configDir,
		onChange:  onChange,
		notify:    notify,
		cancelled: map[string]string{},
		checked:   map[string]string{},
		kick:      make(chan struct{}, 1),
	}
	e.snap.Phase = PhaseIdle
	for label, st := range store.DeviceStates() {
		if st.CancelledConnection != "" {
			e.cancelled[label] = st.CancelledConnection // cancels survive an app restart
		}
	}
	if h := store.History(); len(h) > 0 {
		e.snap.Last = &h[0]
	}
	return e
}

// Run polls for devices every 2 seconds (and immediately when kicked) until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		e.scan()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-e.kick:
		}
	}
}

func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked()
}

func (e *Engine) snapshotLocked() Snapshot {
	s := e.snap
	settings := e.store.Settings()
	s.Paused = settings.Paused
	s.SignedIn = gauth.LoadToken() != nil
	s.Current = append([]FileProgress(nil), e.snap.Current...)
	s.Cancelled = nil
	for label := range e.cancelled {
		s.Cancelled = append(s.Cancelled, label)
	}
	return s
}

func (e *Engine) update(change func(*Snapshot)) {
	e.mu.Lock()
	change(&e.snap)
	s := e.snapshotLocked()
	e.mu.Unlock()
	if e.onChange != nil {
		e.onChange(s)
	}
}

// Kick asks for an immediate device check.
func (e *Engine) Kick() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

func (e *Engine) Pause() {
	_ = e.store.UpdateSettings(func(s *config.Settings) { s.Paused = true })
	e.stopJob()
	e.update(func(*Snapshot) {})
}

func (e *Engine) Resume() {
	_ = e.store.UpdateSettings(func(s *config.Settings) { s.Paused = false })
	e.clearCancels()
	e.update(func(*Snapshot) {})
	e.Kick()
}

// Cancel stops the current job and skips the connected devices until they're unplugged.
func (e *Engine) Cancel() {
	e.mu.Lock()
	for _, d := range e.store.Settings().Devices {
		if v, ok := volumes.Find(d.Label); ok {
			e.cancelled[d.Label] = v.ConnectionID
			st := e.store.DeviceState(d.Label)
			st.CancelledConnection = v.ConnectionID
			_ = e.store.SetDeviceState(d.Label, st)
		}
	}
	e.mu.Unlock()
	e.stopJob()
	e.update(func(*Snapshot) {})
}

// CheckNow forgets cancels and failed checks for connected devices, then checks them again.
func (e *Engine) CheckNow() {
	e.clearCancels()
	for _, d := range e.store.Settings().Devices {
		st := e.store.DeviceState(d.Label)
		st.FailedFingerprint = ""
		_ = e.store.SetDeviceState(d.Label, st)
	}
	e.Kick()
}

func (e *Engine) clearCancels() {
	e.mu.Lock()
	labels := make([]string, 0, len(e.cancelled))
	for label := range e.cancelled {
		labels = append(labels, label)
	}
	e.cancelled = map[string]string{}
	e.checked = map[string]string{}
	e.mu.Unlock()
	for _, label := range labels {
		st := e.store.DeviceState(label)
		st.CancelledConnection = ""
		_ = e.store.SetDeviceState(label, st)
	}
}

func (e *Engine) stopJob() {
	e.mu.Lock()
	cancel := e.cancelJob
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (e *Engine) scan() {
	settings := e.store.Settings()
	mounted := map[string]volumes.Volume{}
	for _, v := range volumes.List() {
		mounted[v.Label] = v
	}

	var connected []string
	e.mu.Lock()
	var ended []string
	for label, conn := range e.cancelled {
		// A cancel lasts only while the device stays plugged in.
		if v, ok := mounted[label]; !ok || v.ConnectionID != conn {
			delete(e.cancelled, label)
			ended = append(ended, label)
		}
	}
	for label, conn := range e.checked {
		if v, ok := mounted[label]; !ok || v.ConnectionID != conn {
			delete(e.checked, label) // unplugged or replugged: check it next time
		}
	}
	for _, d := range settings.Devices {
		if _, ok := mounted[d.Label]; ok {
			connected = append(connected, d.Label)
		}
	}
	busy := e.busy
	e.mu.Unlock()
	for _, label := range ended {
		st := e.store.DeviceState(label)
		st.CancelledConnection = ""
		_ = e.store.SetDeviceState(label, st)
	}
	e.update(func(s *Snapshot) { s.Connected = connected })

	if busy || settings.Paused || gauth.LoadToken() == nil {
		return
	}
	for _, d := range settings.Devices {
		v, ok := mounted[d.Label]
		if !ok {
			continue
		}
		e.mu.Lock()
		_, skip := e.cancelled[d.Label]
		if e.checked[d.Label] == v.ConnectionID {
			skip = true
		}
		e.mu.Unlock()
		if skip {
			continue
		}
		e.startJob(d, v)
		return // one device at a time; the next scan picks up the others
	}
}

func (e *Engine) startJob(d config.Device, v volumes.Volume) {
	ctx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.busy = true
	e.cancelJob = cancel
	e.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			e.mu.Lock()
			e.busy = false
			e.cancelJob = nil
			e.checked[d.Label] = v.ConnectionID
			e.mu.Unlock()
			e.Kick() // another device may be waiting
		}()
		e.runJob(ctx, d, v)
	}()
}

type localFile struct {
	rel  string
	size int64
}

func (e *Engine) runJob(ctx context.Context, d config.Device, v volumes.Volume) {
	settings := e.store.Settings()
	src := v.Path
	if d.Folder != "" && d.Folder != "." {
		src = filepath.Join(v.Path, d.Folder)
	}
	if _, err := os.Stat(src); err != nil {
		return
	}

	files, err := listLocal(src, settings.SkipFiles)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			e.problem(d, "macOS blocked access to "+d.Label+". Allow CardHaul under System Settings → Privacy & Security → Files & Folders.", nil)
		}
		return
	}
	if len(files) == 0 {
		return
	}
	fingerprint := fingerprintOf(files)
	var totalBytes int64
	for _, f := range files {
		totalBytes += f.size
	}
	log.Printf("%s: %d files (%s) on device, connection %s", d.Label, len(files), humanBytes(totalBytes), v.ConnectionID)
	state := e.store.DeviceState(d.Label)

	e.update(func(s *Snapshot) {
		s.Phase, s.Device, s.Dest, s.Message = PhaseScanning, d.Label, DriveRoot+"/"+d.Dest, "Comparing with Google Drive…"
		s.Current, s.ProblemFiles = nil, nil
	})

	// Comparing with Drive should take seconds; don't let a stuck connection hang the app.
	scanCtx, scanCancel := context.WithTimeout(ctx, 3*time.Minute)
	defer scanCancel()
	started := time.Now()
	dstFs, tokenMap, err := e.driveFs(scanCtx, d)
	if err != nil {
		log.Printf("%s: connecting to Google Drive failed after %s: %v", d.Label, time.Since(started).Round(time.Second), err)
		if ctx.Err() == nil {
			e.problem(d, "Couldn't reach Google Drive: "+friendlyErr(err), nil)
		}
		return
	}
	log.Printf("%s: connected to Google Drive in %s", d.Label, time.Since(started).Round(time.Millisecond))
	defer e.persistToken(tokenMap)
	srcFs, err := local.NewFs(ctx, "device", src, configmap.Simple{})
	if err != nil {
		e.problem(d, err.Error(), nil)
		return
	}

	existing := map[string]bool{}
	started = time.Now()
	if err := operations.ListFn(scanCtx, dstFs, func(o rfs.Object) { existing[o.Remote()] = true }); err != nil && !errors.Is(err, rfs.ErrorDirNotFound) {
		log.Printf("%s: listing the Drive folder failed after %s: %v", d.Label, time.Since(started).Round(time.Second), err)
		if ctx.Err() != nil {
			return
		}
		e.problem(d, "Couldn't list your Drive folder: "+friendlyErr(err), nil)
		return
	}
	log.Printf("%s: %d files already in Drive (listed in %s)", d.Label, len(existing), time.Since(started).Round(time.Millisecond))
	var pending []localFile
	var pendingBytes int64
	for _, f := range files {
		if !existing[f.rel] {
			pending = append(pending, f)
			pendingBytes += f.size
		}
	}

	if len(pending) == 0 && (state.VerifiedFingerprint == fingerprint || state.FailedFingerprint == fingerprint+"|"+v.ConnectionID) {
		e.update(func(s *Snapshot) { s.Phase, s.Device, s.Message = PhaseIdle, "", "" })
		return
	}

	log.Printf("%s: %d new files to upload (%s)", d.Label, len(pending), humanBytes(pendingBytes))
	run := config.Run{Device: d.Label, Started: time.Now(), Files: len(pending), Bytes: pendingBytes}
	jobCtx := filterContext(ctx, settings.SkipFiles)
	group := fmt.Sprintf("job-%d", time.Now().UnixNano())

	// Upload
	if len(pending) > 0 {
		e.notify("Uploading "+d.Label, fmt.Sprintf("%d new files (%s). Keep it connected until you see ✅.", len(pending), humanBytes(pendingBytes)))
		upCtx, ci := rfs.AddConfig(accounting.WithStatsGroup(jobCtx, group+"-up"))
		ci.IgnoreExisting = true
		ci.Transfers = settings.Transfers
		e.update(func(s *Snapshot) {
			s.Phase, s.Message = PhaseUploading, ""
			s.BytesTotal, s.FilesTotal, s.BytesDone, s.FilesDone = pendingBytes, len(pending), 0, 0
		})
		stop := e.watchStats(upCtx, group+"-up", PhaseUploading)
		started = time.Now()
		err = rsync.CopyDir(upCtx, dstFs, srcFs, false)
		stop()
		log.Printf("%s: upload finished in %s (err: %v)", d.Label, time.Since(started).Round(time.Second), err)
		if e.stopped(ctx, d, v, &run) {
			return
		}
		if err != nil {
			e.finish(d, &run, "error", "Upload error: "+err.Error())
			e.problem(d, "Upload error: "+err.Error(), nil)
			return
		}
	}

	// Verify. Re-read settings now: the user may have changed the verify mode during the upload.
	settings = e.store.Settings()
	verifyStart := time.Now()
	vCtx, ci := rfs.AddConfig(accounting.WithStatsGroup(jobCtx, group+"-check"))
	ci.SizeOnly = settings.Verify == config.VerifySize
	e.update(func(s *Snapshot) {
		s.Phase, s.Message, s.Current = PhaseVerifying, "", nil
		s.VerifyMode = string(settings.Verify)
		s.FilesTotal, s.FilesDone, s.BytesTotal, s.BytesDone = len(files), 0, totalBytes, 0
		s.ETA = e.verifyEstimate(totalBytes, settings)
	})
	log.Printf("%s: verifying %d files (%s) by %s", d.Label, len(files), humanBytes(totalBytes), settings.Verify)
	stop := e.watchStats(vCtx, group+"-check", PhaseVerifying)
	var differ, missing, failed bytes.Buffer
	err = operations.Check(vCtx, &operations.CheckOpt{
		Fdst: dstFs, Fsrc: srcFs, OneWay: true,
		Differ: &differ, MissingOnDst: &missing, Error: &failed,
	})
	stop()
	log.Printf("%s: verification finished in %s (err: %v)", d.Label, time.Since(verifyStart).Round(time.Second), err)
	if e.stopped(ctx, d, v, &run) {
		return
	}

	if err == nil {
		if settings.Verify == config.VerifyChecksum && time.Since(verifyStart) > time.Minute {
			measured := float64(totalBytes) / 1e6 / time.Since(verifyStart).Seconds()
			_ = e.store.UpdateSettings(func(s *config.Settings) { s.VerifyMBPerSec = 0.5*s.VerifyMBPerSec + 0.5*measured })
		}
		_ = e.store.SetDeviceState(d.Label, config.DeviceState{VerifiedFingerprint: fingerprint, CancelledConnection: e.store.DeviceState(d.Label).CancelledConnection})
		msg := fmt.Sprintf("%d files are safely in Google Drive. Safe to clear the device.", len(files))
		e.finish(d, &run, "verified", msg)
		e.notify("✅ "+d.Label+": upload verified", msg)
		e.update(func(s *Snapshot) { s.Phase, s.Message = PhaseDone, msg })
		return
	}

	bad := lines(differ.String(), missing.String(), failed.String())
	_ = e.store.SetDeviceState(d.Label, config.DeviceState{
		VerifiedFingerprint: state.VerifiedFingerprint,
		FailedFingerprint:   fingerprint + "|" + v.ConnectionID,
		CancelledConnection: e.store.DeviceState(d.Label).CancelledConnection,
	})
	msg := fmt.Sprintf("%d file(s) didn't verify. The other %d are fine. Don't clear the device yet.", len(bad), len(files)-len(bad))
	if len(bad) == 0 {
		msg = "Verification couldn't finish: " + err.Error()
	}
	e.finish(d, &run, "failed", msg)
	e.notify("⚠️ "+d.Label+": check needed", msg)
	e.problem(d, msg, bad)
}

// stopped reports (and records) a pause, cancel or unplug that interrupted the job.
func (e *Engine) stopped(ctx context.Context, d config.Device, v volumes.Volume, run *config.Run) bool {
	if _, ok := volumes.Find(d.Label); !ok {
		e.finish(d, run, "disconnected", "Disconnected before finishing. Plug it back in to resume.")
		e.notify("⚠️ "+d.Label+" disconnected", "The upload didn't finish. Plug it back in to resume. Don't clear it yet.")
		e.update(func(s *Snapshot) { s.Phase, s.Device, s.Message, s.Current = PhaseIdle, "", "", nil })
		return true
	}
	if ctx.Err() == nil {
		return false
	}
	result, msg := "cancelled", "Cancelled. Uploads again next time it's plugged in."
	if e.store.Settings().Paused {
		result, msg = "paused", "Paused. Files already uploaded are kept."
	}
	e.finish(d, run, result, msg)
	e.update(func(s *Snapshot) { s.Phase, s.Device, s.Message, s.Current = PhaseIdle, "", "", nil })
	return true
}

func (e *Engine) finish(d config.Device, run *config.Run, result, msg string) {
	log.Printf("%s: run finished: %s. %s", d.Label, result, msg)
	run.Finished, run.Result, run.Message = time.Now(), result, msg
	_ = e.store.AddRun(*run)
	r := *run
	e.update(func(s *Snapshot) { s.Last = &r })
}

func (e *Engine) problem(d config.Device, msg string, files []string) {
	log.Printf("%s: problem: %s %v", d.Label, msg, files)
	e.update(func(s *Snapshot) {
		s.Phase, s.Device, s.Message, s.ProblemFiles, s.Current = PhaseProblem, d.Label, msg, files, nil
	})
}

// watchStats copies rclone's live stats into the snapshot every second until stopped.
func (e *Engine) watchStats(ctx context.Context, group string, phase Phase) (stop func()) {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		started := time.Now()
		for {
			select {
			case <-done:
				return
			case <-t.C:
			}
			raw, err := accounting.StatsGroup(ctx, group).RemoteStats(false)
			if err != nil {
				continue
			}
			var st struct {
				Bytes          int64    `json:"bytes"`
				TotalBytes     int64    `json:"totalBytes"`
				Speed          float64  `json:"speed"`
				ETA            *float64 `json:"eta"`
				Transfers      int      `json:"transfers"`
				Checks         int      `json:"checks"`
				TotalChecks    int      `json:"totalChecks"`
				Checking       []string `json:"checking"`
				Transferring   []struct {
					Name       string `json:"name"`
					Percentage int    `json:"percentage"`
				} `json:"transferring"`
			}
			data, _ := json.Marshal(raw)
			_ = json.Unmarshal(data, &st)
			settings := e.store.Settings()
			e.update(func(s *Snapshot) {
				s.Elapsed = time.Since(started).Seconds()
				s.Current = nil
				if phase == PhaseUploading {
					s.BytesDone, s.Speed, s.FilesDone = st.Bytes, st.Speed, st.Transfers
					if st.TotalBytes > s.BytesTotal {
						s.BytesTotal = st.TotalBytes
					}
					s.ETA = -1
					if st.ETA != nil {
						s.ETA = *st.ETA
					} else if st.Speed > 0 {
						s.ETA = float64(s.BytesTotal-s.BytesDone) / st.Speed
					}
					for _, tr := range st.Transferring {
						s.Current = append(s.Current, FileProgress{Name: tr.Name, Percent: tr.Percentage})
					}
					sort.Slice(s.Current, func(i, j int) bool { return s.Current[i].Percent > s.Current[j].Percent })
				} else {
					s.FilesDone = st.Checks
					for _, name := range st.Checking {
						s.Current = append(s.Current, FileProgress{Name: name})
					}
					// Checksums read every byte back from the device: estimate from the measured speed.
					// Use the mode this check started with, not whatever the setting says now.
					mode := settings
					mode.Verify = config.VerifyMode(s.VerifyMode)
					s.ETA = e.verifyEstimate(s.BytesTotal, mode) - s.Elapsed
					if mode.Verify == config.VerifySize || s.ETA < 0 {
						s.ETA = -1
					}
				}
			})
		}
	}()
	return func() { close(done) }
}

func (e *Engine) verifyEstimate(totalBytes int64, settings config.Settings) float64 {
	if settings.Verify == config.VerifySize {
		return 5
	}
	return float64(totalBytes) / 1e6 / settings.VerifyMBPerSec
}

// driveFs connects to <DriveRoot>/<device dest> in Google Drive with the saved token.
func (e *Engine) driveFs(ctx context.Context, d config.Device) (rfs.Fs, configmap.Simple, error) {
	token := gauth.LoadToken()
	if token == nil {
		return nil, nil, errors.New("not signed in")
	}
	id, secret, err := gauth.Client(e.configDir)
	if err != nil {
		return nil, nil, err
	}
	tokenJSON, _ := json.Marshal(token)
	m := configmap.Simple{
		"scope":         "drive.file",
		"client_id":     id,
		"client_secret": secret,
		"token":         string(tokenJSON),
		"chunk_size":    "64M",
	}
	f, err := drive.NewFs(ctx, "cardhaul", path.Join(DriveRoot, d.Dest), m)
	if errors.Is(err, rfs.ErrorIsFile) {
		err = nil
	}
	return f, m, err
}

// persistToken saves a token rclone refreshed during the job back to the keychain.
func (e *Engine) persistToken(m configmap.Simple) {
	if m == nil {
		return
	}
	var token oauth2.Token
	if raw, ok := m["token"]; ok && json.Unmarshal([]byte(raw), &token) == nil && token.RefreshToken != "" {
		_ = gauth.SaveToken(&token)
	}
}

func filterContext(ctx context.Context, skip []string) context.Context {
	fi, err := filter.NewFilter(nil)
	if err != nil {
		return ctx
	}
	_ = fi.AddRule("- .*")
	_ = fi.AddRule("- .*/**")
	for _, p := range skip {
		_ = fi.AddRule("- " + p)
	}
	return filter.ReplaceConfig(ctx, fi)
}

func listLocal(root string, skip []string) ([]localFile, error) {
	var out []localFile
	err := filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil
		}
		name := entry.Name()
		if p != root && strings.HasPrefix(name, ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		for _, pattern := range skip {
			if ok, _ := filepath.Match(pattern, name); ok {
				return nil
			}
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, localFile{rel: filepath.ToSlash(rel), size: info.Size()})
		return nil
	})
	if err == nil {
		// Reading the directory can still be blocked by macOS privacy controls.
		if _, readErr := os.ReadDir(root); readErr != nil {
			return nil, readErr
		}
	}
	return out, err
}

func fingerprintOf(files []localFile) string {
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%s\x00%d\n", f.rel, f.size)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func lines(chunks ...string) []string {
	var out []string
	for _, c := range chunks {
		for _, l := range strings.Split(c, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				out = append(out, l)
			}
		}
	}
	return out
}

// friendlyErr turns a timeout into something a person can act on.
func friendlyErr(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "Google Drive didn't respond within 3 minutes. Check your internet connection, then choose Check again."
	}
	return err.Error()
}

func humanBytes(n int64) string {
	f := float64(n)
	for _, unit := range []string{"B", "KB", "MB", "GB"} {
		if f < 1000 {
			return fmt.Sprintf("%.0f %s", f, unit)
		}
		f /= 1000
	}
	return fmt.Sprintf("%.1f TB", f)
}
