# SPEC: Battery Monitoring and Safe Shutdown (MAX17048)

## Status
Draft. Implements `PRD_BATTERY_MONITOR.md`. Every section lists the PRD
requirement IDs it covers. Tasks at the end are the unit of implementation;
each task is one PR (or one commit on `feat/battery-monitor`) with its tests.

## 1. Overview

```
            +-------------------- raspicam-ui ---------------------+
 MAX17048   |                                                      |
 (I2C 0x36) |  battery.Gauge --> battery.Monitor --+--> UI cache    |
 /dev/i2c-1 |   (max17048.go)    (ticker, policy)  |    (Latest)    |
    <-------+                          |           +--> history     |
            |                          v                Recorder    |
            |                  battery.Shutdown <-- web /power-off  |
            |                   (log, flush,                        |
            |                    poweroff)                          |
            |                          |                            |
            +--------------------------|----------------------------+
                                       v
                           system.PowerOff -> systemctl poweroff
```

- One goroutine (the monitor) owns the gauge and polls it on a ticker.
- The UI never touches I2C; it reads the monitor's latest snapshot.
- Both shutdown triggers (low battery, UI button) go through one
  `battery.Shutdown` coordinator.
- Phase 2 adds a `history.Recorder` that receives every reading from the
  monitor through a `Sink` interface.

## 2. Package Layout

| Package | Files | Responsibility |
|---|---|---|
| `internal/battery` | `max17048.go` | Register map, decoding, `Reading` type |
| | `i2c_linux.go`, `i2c_other.go` | `/dev/i2c-N` access (linux), stub elsewhere |
| | `policy.go` | Pure shutdown decision logic (thresholds, hysteresis, grace) |
| | `trend.go` | Charging / discharging / idle classification |
| | `monitor.go` | Polling loop, latest snapshot, sinks |
| | `shutdown.go` | Shutdown coordinator (log, flush, power off, dry run) |
| | `shutdownlog.go` | Last-10 shutdown log (JSON lines) |
| `internal/battery/history` | `store.go`, `session.go`, `stats.go`, `recorder.go`, `offline.go` | Phase 2 recording, sessions, retention, statistics |
| `internal/config` | `battery.go` | Env loading and validation for all battery settings |
| `internal/system` | `power.go`, `timesync.go`, `boot.go` | `systemctl poweroff`, permission check, NTP sync flag, boot id and uptime |
| `internal/web` | `battery.go`, `templates/battery_card.html`, `templates/shutdown.html`, `templates/battery.html`, `svg.go` | Handlers, view models, templates, SVG chart points |
| `cmd/ui` | `main.go` | Wiring, signal handling, graceful shutdown |

New code follows the existing style: strict parsing functions with table
tests, `fmt.Errorf("...: %w", err)`, dependencies injected via constructors,
no globals.

## 3. Phase 1 Design

### 3.1 MAX17048 Access (BAT-FR-1..5, BAT-NFR-1, BAT-NFR-7, Q2, Q5)

Registers used (16-bit, big-endian, MSB first):

| Reg | Name | Decode | Used for |
|---|---|---|---|
| 0x02 | VCELL | `raw * 78.125e-6` V | voltage |
| 0x04 | SOC | `raw / 256.0` % | state of charge |
| 0x08 | VERSION | raw | detection at startup |
| 0x16 | CRATE | `int16(raw) * 0.208` %/h | trend, time remaining |
| 0x1A | STATUS | bit `RI` (0x0100) | reset indicator (low confidence) |

```go
// Reading is one decoded sample from the gauge.
type Reading struct {
    Time   time.Time // wall clock with monotonic component
    Volts  float64
    SOC    float64 // raw, may exceed 100; clamp only for display
    CRate  float64 // %/hour, negative while discharging
}

type Gauge interface {
    Read(ctx context.Context) (Reading, error)
}

// Pure decoding helpers, table-tested.
func decodeVCell(b [2]byte) float64
func decodeSOC(b [2]byte) float64
func decodeCRate(b [2]byte) float64
func plausible(r Reading) error // volts in [2.5, 4.5], SOC in [0, 110]
```

I2C transport:
- `type Bus interface { ReadRegs(addr uint16, reg byte, buf []byte) error; Close() error }`.
- `i2c_linux.go`: `OpenBus(path string) (*LinuxBus, error)` opens the device
  once and keeps the fd open (BAT-NFR-7). `ReadRegs` uses the `I2C_RDWR`
  ioctl (`0x0707`) with two messages (write register, read N bytes) so the
  read is a single combined transaction with repeated start, as the MAX17048
  requires. Implemented with `syscall.Syscall(SYS_IOCTL, ...)` and a fixed
  `[2]i2cMsg` array: no allocations per read. The message array and the
  register/data buffers live in the heap-allocated `LinuxBus` struct (never
  on the stack, which can move), and the call keeps them alive with
  `runtime.KeepAlive`. The kernel struct layout (`i2c_msg`,
  `i2c_rdwr_ioctl_data`) is for 32-bit ARM; a size test guards it.
- `i2c_other.go` (`//go:build !linux`): `OpenBus` returns
  `errors.ErrUnsupported`, so `go run ./cmd/ui` on macOS works in degraded
  mode.
- Per poll: one 4-byte read starting at 0x02 (VCELL + SOC, auto-increment)
  and one 2-byte read of 0x16 (CRATE). Two short transactions, well under
  1ms total. (The PRD's "single 6-byte transaction" is corrected here: CRATE
  is not adjacent to SOC.)
- `NewMAX17048(bus Bus, addr uint16) (*MAX17048, error)` reads VERSION and
  STATUS once. Unexpected VERSION (not `0x001x`) logs a warning but does not
  fail. `RI` set marks the gauge as `ResetSeen` (used by phase 2).
- No writes to the chip (Q5). The chip's auto-hibernate stays at defaults.
- Concurrency: only the monitor goroutine calls `Read` (BAT-FR-5). Each
  `Read` is bounded by a 200ms timeout via `ctx`; the ioctl itself is
  blocking, so the check happens before each transaction.

### 3.2 Trend (BAT-FR-6)

```go
type Trend int // TrendUnknown, TrendIdle, TrendCharging, TrendDischarging

// classifyTrend uses CRATE with a dead band and requires 2 consecutive
// samples to change state.
type TrendTracker struct{ ... }
func (t *TrendTracker) Update(crate float64) Trend
```

- Dead band: `|CRATE| < 0.5 %/h` -> idle.
- `CRATE >= +0.5` -> charging, `<= -0.5` -> discharging.
- State changes after 2 consecutive samples in the new band.

Time remaining (discharging only) is the time until the shutdown threshold,
not until 0%: `(SOC - ShutdownSOC) / |CRATE|` hours. It is shown when
`|CRATE| >= 1 %/h` and the trend has been `discharging` for at least 5
minutes; otherwise hidden.

### 3.3 Shutdown Policy (BAT-FR-9, BAT-FR-10, BAT-FR-13, Q3)

Pure, clock-injected, no I/O:

```go
type PolicyConfig struct {
    LowSOC          float64       // 20
    ShutdownSOC     float64       // 5
    ShutdownVolts   float64       // 3.30
    RequiredSamples int           // 3
    GracePeriod     time.Duration // 2m
}

type Level int // LevelOK, LevelLow, LevelCritical

type Decision struct {
    Level    Level
    Shutdown bool
    Reason   string // "soc" or "volts" when Shutdown
}

type Policy struct{ cfg PolicyConfig; lowCount int; latched bool }

func NewPolicy(cfg PolicyConfig) *Policy
// uptime is the system uptime (from /proc/uptime), not process run time.
func (p *Policy) Evaluate(r Reading, readErr error, trend Trend, uptime time.Duration) Decision
```

Rules, in order:
1. `readErr != nil` or `plausible(r) != nil`: `lowCount` unchanged, no
   shutdown. A read error never counts as low (BAT-FR-10).
2. `uptime < GracePeriod`: level computed, `lowCount = 0`, no shutdown. The
   grace period is measured from system boot, not from UI start, so a UI
   crash and restart at low battery does not add another grace period.
3. `trend == TrendCharging`: `lowCount = 0`, `latched = false`, no shutdown.
   (Clearing the latch lets dry-run calibration repeat after recharging.)
4. Low condition: `SOC <= ShutdownSOC || Volts <= ShutdownVolts`. If true,
   `lowCount++`, else `lowCount = 0`.
5. `lowCount >= RequiredSamples` -> `Shutdown = true` and `latched = true`;
   while latched, further calls keep returning `Shutdown = true`.

Level: `Critical` when the low condition holds, `Low` when
`SOC <= LowSOC`, else `OK`.

Shutdown latency at defaults: 3 samples x 30s = 90s (BAT-NFR-7).

### 3.4 Monitor (BAT-FR-7, BAT-FR-8, BAT-FR-12, BAT-NFR-2, BAT-NFR-3, BAT-NFR-8, Q1)

```go
type State int // StateDisabled, StateNotDetected, StateReadError, StateOK

type Status struct {
    State     State
    Err       string    // reason for NotDetected / ReadError
    Reading   Reading   // last good reading
    ReadAt    time.Time // time of last good reading
    Trend     Trend
    Decision  Decision
    Remaining time.Duration // 0 when hidden
    ResetSeen bool
}

type Sink interface {
    Record(r Reading, s Status) // must not block; called on monitor goroutine
}

type Monitor struct { /* gauge, policy, trend, sinks, shutdown, mu, status */ }

// uptime returns system uptime (system.Uptime in main, a fake in tests).
func NewMonitor(g Gauge, p *Policy, sd *Shutdown, interval time.Duration,
    uptime func() (time.Duration, error), sinks ...Sink) *Monitor
func (m *Monitor) Run(ctx context.Context)    // blocks until ctx done
func (m *Monitor) Status() Status             // RWMutex-protected copy
```

- `Run` reads once immediately, then on a single `time.Ticker`.
- After 3 consecutive read errors the state becomes `ReadError` (UI shows
  the error); a successful read restores `StateOK`.
- If the gauge was never detected, `Run` is not started; `Status()` returns
  `StateNotDetected` with the open error, or `StateDisabled` when
  `BATTERY_ENABLED=false`. In both cases no shutdown can trigger
  (BAT-FR-12).
- When `Decision.Shutdown` is true, the monitor calls
  `sd.Execute(ctx, Event{Reason: ReasonLowBattery, ...})`. If `Execute`
  returns a power-off error, the monitor retries on the next tick (the
  decision stays latched), so a transient `systemctl` failure does not leave
  the Pi running until the cells die. `ErrShutdownInProgress` and dry-run
  completion stop further calls until the latch clears.
- Sinks are called synchronously on the monitor goroutine and must use their
  own lock; `Shutdown` may call a sink's `Flush` from the same goroutine, so
  sinks must not call back into the monitor.
- The UI reads `Status()` only; page loads never cause I2C reads
  (BAT-FR-7, BAT-NFR-7).
- Steady state: no allocations per tick other than the status copy.

### 3.5 Shutdown Coordinator (BAT-FR-11, BAT-FR-14, BAT-FR-17, BAT-FR-18)

```go
type Reason string // ReasonLowBattery = "low_battery", ReasonManual = "manual"

type Event struct {
    Time      time.Time
    Reason    Reason
    Threshold string  // "soc" / "volts" for low_battery, "" for manual
    Volts     float64 // 0 when unknown (gauge absent)
    SOC       float64
    HasBattery bool
    Synced    bool
    Client    string // remote address for manual
    DryRun    bool
}

type PowerOff interface {
    PowerOff(ctx context.Context) error
}

type Flusher interface {
    Flush(ctx context.Context) error
}

type Shutdown struct { /* log *ShutdownLog, power PowerOff, flushers, dryRun, once */ }

func NewShutdown(log *ShutdownLog, power PowerOff, dryRun bool, flushers ...Flusher) *Shutdown
func (s *Shutdown) Execute(ctx context.Context, ev Event) error
func (s *Shutdown) Pending() bool
```

`Execute` steps:
1. Guard (real mode, mutex-protected state, not `sync.Once`): while a
   shutdown is in progress, other calls return `ErrShutdownInProgress`. If
   `power.PowerOff` fails, the guard is released so the call can be retried
   (by the monitor's next tick or another button press). Dry run allows
   repeated events but logs each.
2. `log.Printf` with reason, threshold, voltage, SOC, client, dry run.
3. Append the event to the shutdown log (fsync before continuing). On a
   retry after a failed power-off, the event is not appended again.
4. Run all flushers (phase 2 history) with a 5s total timeout; errors are
   logged, not fatal. Flushers must be idempotent: systemd will SIGTERM the
   UI during poweroff and the normal exit path flushes again.
5. Dry run: stop here. Real: `power.PowerOff(ctx)`. `systemctl poweroff`
   returns once the job is queued; systemd then stops the UI with SIGTERM.

`Execute` uses its own context (`context.Background()` with a 15s timeout),
never a request context, so it is not cancelled when the HTTP response
finishes.

### 3.6 Shutdown Log (BAT-FR-18, Q6)

- File: `<state dir>/shutdowns.jsonl` (default state dir
  `/var/lib/raspicam`, from `STATE_DIRECTORY` set by systemd, overridable).
- One JSON object per line (the `Event` fields, snake_case).
- `Append(ev)`: read existing lines, skip corrupt ones (log), append new,
  keep the last 10, write to `shutdowns.jsonl.tmp`, `fsync`, `rename`,
  `fsync` the directory.
- `List() ([]Event, error)`: newest first.
- Missing file -> empty list, no error.

### 3.7 Power Off and Permissions (BAT-FR-11, BAT-FR-17)

`internal/system/power.go`:

```go
type SystemdPower struct{ /* runner for exec */ }
func (p SystemdPower) PowerOff(ctx context.Context) error // systemctl poweroff
func CanPowerOff(ctx context.Context) (bool, string)       // checked once at startup
```

- `CanPowerOff` runs
  `busctl call org.freedesktop.login1 /org/freedesktop/login1
  org.freedesktop.login1.Manager CanPowerOff` and parses `s "yes"`. Any other
  answer (`no`, `challenge`, error) returns false with the reason.
- Result is cached at startup; the UI shows the power-off button disabled
  with the reason when false. The low-battery path still tries to power off
  (it may succeed if permissions changed) and logs the error otherwise.
- Output parsing is a pure function with tests.

Polkit rule installed by the operator (documented in `SYSTEM.md`):

```
// /etc/polkit-1/rules.d/50-raspicam-poweroff.rules
polkit.addRule(function(action, subject) {
  if ((action.id == "org.freedesktop.login1.power-off" ||
       action.id == "org.freedesktop.login1.power-off-multiple-sessions") &&
      subject.user == "pi") { // must match User= in the systemd unit
    return polkit.Result.YES;
  }
});
```

### 3.8 Configuration (BAT-FR-15, BAT-FR-16, BAT-NFR-7)

`internal/config/battery.go`:

```go
type BatteryConfig struct { ... }
func LoadBatteryConfig(getenv func(string) string) (BatteryConfig, []string)
```

`getenv` is injected (`os.Getenv` in main, a map in tests). Invalid values
are replaced by defaults and returned as warnings (logged at startup and
shown in the status page warnings).

| Variable | Default | Valid |
|---|---|---|
| `BATTERY_ENABLED` | `true` | bool |
| `BATTERY_I2C_DEVICE` | `/dev/i2c-1` | non-empty path |
| `BATTERY_I2C_ADDR` | `0x36` | 0x03-0x77 |
| `BATTERY_POLL_INTERVAL` | `30s` | 10s-10m |
| `BATTERY_LOW_SOC` | `20` | 1-90 |
| `BATTERY_SHUTDOWN_SOC` | `5` | 0-50, `< LOW_SOC` |
| `BATTERY_SHUTDOWN_VOLTS` | `3.30` | 3.00-3.80 |
| `BATTERY_SHUTDOWN_SAMPLES` | `3` | 1-20 |
| `BATTERY_GRACE_PERIOD` | `2m` | 0-30m |
| `BATTERY_SHUTDOWN_DRY_RUN` | `false` | bool |
| `STATE_DIR` | `$STATE_DIRECTORY` or `/var/lib/raspicam` | path |
| `BATTERY_HISTORY_ENABLED` | `true` | bool (phase 2) |
| `BATTERY_HISTORY_SAMPLE_INTERVAL` | `60s` | 30s-10m, multiple of poll interval |
| `BATTERY_HISTORY_FLUSH_INTERVAL` | `15m` | 1m-1h |
| `BATTERY_HISTORY_MAX_SIZE` | `10MB` | 1MB-500MB |
| `BATTERY_FULL_SOC` | `95` | 50-100 |

A poll interval below 10s is rejected (replaced by the default) per AC-9.

### 3.9 Web (BAT-FR-6, BAT-FR-12, BAT-FR-17, BAT-NFR-6)

Server construction changes from `NewServer()` to `NewServer(deps Deps)`:

```go
type BatteryStatusProvider interface { Status() battery.Status }
type ShutdownRequester interface {
    Execute(ctx context.Context, ev battery.Event) error
    Pending() bool
}

type Deps struct {
    Battery      BatteryStatusProvider // never nil; a disabled stub when off
    Shutdown     ShutdownRequester
    ShutdownLog  ShutdownLister
    CanPowerOff  bool
    PowerOffNote string
    DryRun       bool
    // existing settings (MediaMTX URL, path, config path) move here too
}
```

Status page: a new Battery card (`templates/battery_card.html`, included by
`status.html`), placed after the top metric rows:
- Voltage `3.87 V`, SOC `64%` with a CSS bar, trend, `~3h 20m` remaining,
  level badge (`badge ok|warn|err`), last reading time.
- Auto-shutdown line: `Shutdown at 5% or 3.30 V (dry run)`, or
  `Inactive: gauge not detected`.
- Last shutdown (from the log) with reason and time.
- Power-off form (below).
- Link to `/battery` (phase 2).
- Degraded states show `Disabled`, `Not detected: <reason>`, or
  `Read error: <reason>`; the card never breaks the page.

`POST /power-off` (BAT-FR-17):
- Form fields: `confirm=yes` (checkbox "I understand the Pi will power off").
- Not POST -> 405. Missing confirm -> redirect `/?power=confirm-required`
  (PRG, like camera config).
- `CanPowerOff == false` and not dry run -> redirect `/?power=not-permitted`.
- Valid: build `Event{Reason: manual, Client: r.RemoteAddr, ...}` from the
  current status, render `shutdown.html` ("Shutting down. Wait for the green
  activity LED to stop blinking before cutting power."), flush the response,
  then run `Execute` in a goroutine after 1s so the page reaches the browser.
  The goroutine uses its own context (see 3.5), not `r.Context()`.
- If `Execute` returns `ErrShutdownInProgress`, render the same shutdown page.
- Dry run: redirect `/?power=dry-run` and the banner says the request was
  logged only.
- Cross-site request protection: without it, any web page open in a browser
  on the same network could submit this form. State-changing POSTs
  (`/power-off`, and `/battery/reset` in phase 2) are rejected with 403
  unless `Sec-Fetch-Site` is `same-origin` or `none`, or, when that header is
  absent, the `Origin` header host matches `r.Host`. Uses the
  `sameOrigin(next http.Handler)` middleware in `internal/web/csrf.go`, added
  for `/camera-config` in a separate PR (`fix/camera-config-csrf`); this
  feature reuses it. If that PR has not merged when P1-8 starts, rebase on it
  first. Go 1.22 has no built-in equivalent.

### 3.10 Main Wiring (BAT-NFR-3, Q1)

`cmd/ui/main.go`:
1. `ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)`.
2. Load battery config; log warnings.
3. Open the state dir (create if missing), shutdown log, `SystemdPower`,
   `CanPowerOff`.
4. If enabled: `OpenBus` + `NewMAX17048`. On error: log, keep a
   not-detected status provider.
5. Phase 2: build the history recorder (a `Sink` and a `Flusher`).
6. `NewShutdown`, `NewPolicy`, `NewMonitor`; `go monitor.Run(ctx)`.
7. HTTP server in a goroutine; on `ctx.Done()`: `httpServer.Shutdown` with
   5s timeout, then flush history, close the bus.

systemd unit changes: `Restart=always`, `RestartSec=2`,
`StateDirectory=raspicam`, `SupplementaryGroups=i2c`, battery env vars.

## 4. Phase 2 Design

### 4.1 Storage Layout (BAT-H-FR-2..5, BAT-H-NFR-1..2, Q10)

Under `<state dir>/battery/`:

| File | Format | Notes |
|---|---|---|
| `samples-YYYY-MM-DD.csv` | CSV, one line per sample | daily files, size-capped |
| `sessions.jsonl` | JSON lines, one closed session per line | kept forever |
| `open-session.json` | JSON, current session | rewritten on each flush |
| `meta.json` | JSON: last sample (time, uptime, boot id, SOC) | for off-line charge detection |
| `archive-<unix>/` | moved files | created by reset |

Sample line (no header, about 34 bytes, about 50 KB/day at 60s, so 10 MB is
about 6-7 months):

```
<unix_s>,<uptime_s>,<mV>,<soc_x10>,<crate_x100>,<trend d|c|i>,<synced 0|1>
1767225600,5231,3912,874,-1250,d,1
```

Integers only: parsing is strict and allocation-light.

Write rules:
- Samples are buffered in a fixed-capacity slice (default flush 15m / 60s =
  15 entries; capacity 64 to cover config ranges).
- Flush triggers: flush interval ticker, session change, `Flush()` from the
  shutdown coordinator, clean exit. One `open(O_APPEND)`, one write, one
  `fsync`, close per flush (BAT-H-NFR-1).
- Retention after each flush: if the total size of `samples-*.csv` exceeds
  `MaxSize`, delete oldest files until it fits; today's file is never
  deleted (BAT-H-FR-4).
- Low disk: if free space on the state dir filesystem (via
  `syscall.Statfs`) is under 100 MB, skip writing samples and set a warning
  (sessions and the shutdown log are still written; they are tiny).
- Readers skip corrupt or truncated lines and count them (BAT-H-FR-5).

### 4.2 Time Handling (BAT-H-FR-10, BAT-H-FR-11, Q11)

`internal/system`:
- `BootID() (string, error)`: `/proc/sys/kernel/random/boot_id`.
- `Uptime() (time.Duration, error)`: first field of `/proc/uptime`.
- `TimeSynced() bool`: `/run/systemd/timesync/synchronized` exists.

Durations:
- Same boot (same boot id): `uptime_end - uptime_start`. Correct even if wall
  time jumps after NTP sync, and across UI restarts.
- Across boots: wall clock difference; marked `reliable=false` unless both
  ends were `synced`.

### 4.3 Session Detection (BAT-H-FR-6..8, BAT-H-FR-9a)

Pure state machine fed with `(Reading, Trend, uptime, bootID, synced)`:

```go
type Session struct {
    State       string    // "discharging" | "charging"
    Start, End  time.Time
    StartUptime, EndUptime time.Duration
    BootIDs     []string  // more than one when resumed across a reboot
    StartSOC, EndSOC     float64
    StartVolts, EndVolts float64
    MinVolts    float64
    Duration    time.Duration
    AvgRate     float64   // %/hour
    EndReason   string    // low_battery_shutdown | manual_shutdown | charger_connected |
                          // charger_removed | full | reboot | unknown
    Reliable    bool
    Full        bool      // discharging from >= FullSOC to low_battery_shutdown
    UnderLoad   bool      // charging with the Pi running (always true when measured)
    LowConfidence bool    // gauge reset seen (STATUS.RI)
}

type Detector struct{ ... }
func (d *Detector) Observe(s Sample) (closed *Session)
func (d *Detector) Close(reason string, s Sample) *Session
```

- Uses the trend from 3.2; a session starts when the trend has been
  charging/discharging for 3 consecutive history samples, and ends when a
  different trend holds for 3 samples.
- Idle periods are not sessions. Sessions shorter than 5 minutes are
  dropped.
- `full`: charging session ends with SOC >= 98 and trend idle.
- Shutdown: the coordinator's `Flush` closes the open session with
  `low_battery_shutdown` or `manual_shutdown` (the reason is passed via the
  Flusher call context, see task P2-5).
- Resume (BAT-H-FR-8): at startup, load `open-session.json`. If the first new
  sample is within 10 minutes of its last sample (wall time, or uptime when
  same boot), continue it; otherwise close it with `unknown` (or `reboot`
  if the boot id changed and the gap is short).

### 4.4 Off-line Charge Inference (BAT-H-FR-9, Q8)

At startup, after the first good reading:
- Load `meta.json` (last sample before the previous shutdown).
- If `SOC_now - SOC_last >= 10` points: append a session with
  `State=charging`, `Inferred=true`, start/end SOC, `Full = SOC_now >= 98`,
  and `Duration` = wall time difference as an upper bound, `Reliable` only if
  both times were synced.
- If the gauge reports `RI` (it was reset, e.g. cells removed), mark it
  `LowConfidence`.

### 4.5 Statistics (BAT-H-FR-12, BAT-H-NFR-3)

Computed on `/battery` load from `sessions.jsonl` only (small: one line per
session):
- Last full discharge: runtime, start/end SOC and voltage.
- Average and latest runtime over the last 5 `Full && Reliable` discharges.
- Runtime per full cycle (list for the "over time" table/chart).
- Average discharge rate (all reliable discharging sessions).
- Average charge time and rate from measured (`!Inferred`) charging
  sessions only.
- Last charge: charge added, reached full, duration (measured) or `<= X`
  (inferred).

### 4.6 `/battery` Page (BAT-H-FR-12..15, Q9)

- `GET /battery`: statistics, sessions table (newest 20), shutdown log
  (10), "samples since <date> (<N> days)", chart of the current or last
  session.
- Chart (BAT-H-FR-13): `svg.go` reads only the sample files covering that
  session, downsamples to at most 300 points, and returns polyline point
  strings for voltage and SOC. Rendered as inline `<svg>` in the template.
  No JavaScript.
- `GET /battery/samples.csv`: streams all sample files in order
  (`io.Copy`, no buffering of the whole set).
- `GET /battery/sessions.jsonl`: streams the file.
- `POST /battery/reset` with `confirm=yes` (behind `sameOrigin`): moves all history files into
  `archive-<unix>/`, starts fresh; PRG redirect to `/battery?reset=done`.
  The shutdown log is not reset.

## 5. Testing (BAT-NFR-5, BAT-H-NFR-4)

All tests run without hardware (`go test ./...` on macOS and Linux).

| Area | Tests |
|---|---|
| Decoding | table tests for VCELL/SOC/CRATE incl. 0, max, negative CRATE, datasheet examples |
| Plausibility | out-of-range volts and SOC rejected |
| Trend | dead band, 2-sample switch, noise around the band |
| Policy | grace period by uptime (incl. UI restart after grace), charging suppression and latch clear, N consecutive, reset on recovery, read errors never count, latch, volts-only and SOC-only triggers |
| Monitor | fake gauge, injected `tick <-chan time.Time` and uptime func, read-error state transitions, power-off failure retried on next tick, no retry after success |
| Shutdown | ordering (log before flush before power), dry run never calls power, second call rejected while in progress, guard released and no duplicate log entry after power-off failure |
| Shutdown log | append keeps last 10, corrupt lines skipped, atomic rewrite, newest first |
| Config | defaults, each invalid value replaced with warning, `SHUTDOWN_SOC < LOW_SOC`, poll interval < 10s rejected |
| Power | `busctl` output parsing (`s "yes"`, `s "no"`, `s "challenge"`, garbage) |
| Web | battery card in each state, `/power-off` method, missing confirm, not permitted, dry run, in progress, success renders shutdown page and calls fake requester; `sameOrigin` accepts same-origin/none/matching Origin and rejects cross-site |
| History store | sample line format/parse, corrupt lines, flush batching count, retention by size keeps today, low disk skip |
| Sessions | start/end hysteresis, short sessions dropped, full detection, resume same boot, resume across reboot, gap closes as unknown |
| Off-line charge | inferred record, threshold, RI low confidence, unsynced unreliable |
| Stats | averages exclude inferred and unreliable, last-5 window |
| SVG | downsampling bounds, scaling to viewBox |

Hardware acceptance (AC-1, AC-2, AC-4, AC-5, AC-9, AC-10, AC-H-1..3a) is a
manual checklist run on the Pi, recorded in the PR description.

## 6. Documentation Changes

- `SYSTEM.md`: wiring, I2C enablement, `i2c` group, polkit rule, systemd
  changes, dry-run calibration procedure (Q3, Q7).
- `PRD.md`: battery in the "Shows" list, auto-shutdown, `/battery`.
- `README.md`: env vars and endpoints (`POST /power-off`, `/battery`,
  downloads, reset).
- `systemd/raspicam-ui.service`: see 3.10.
- `PRD_BATTERY_MONITOR.md`: BAT-NFR-7 transaction wording corrected (two
  short reads, not one).

## 7. Implementation Tasks

Phase 1:

| Task | Scope | Covers |
|---|---|---|
| P1-1 | `battery/max17048.go`: registers, decode, plausibility, `Gauge`; tests | BAT-FR-3, 4 |
| P1-2 | `battery/i2c_linux.go`, `i2c_other.go`: `I2C_RDWR` bus; `NewMAX17048` detection | BAT-FR-1, 2, 5, BAT-NFR-1, 7 |
| P1-3 | `config/battery.go`: loading, validation; tests | BAT-FR-15, 16 |
| P1-4 | `battery/trend.go`, `battery/policy.go`; tests | BAT-FR-9, 10, 13 |
| P1-5 | `battery/shutdownlog.go`, `battery/shutdown.go`; tests | BAT-FR-11, 14, 18 |
| P1-6 | `system/power.go`: poweroff, `CanPowerOff`; `system/boot.go`: `Uptime`; tests | BAT-FR-10, 11, 17 |
| P1-7 | `battery/monitor.go`; tests | BAT-FR-7, 8, 12, BAT-NFR-2, 3, 8 |
| P1-8 | `web`: `Deps`, battery card, `/power-off` (behind existing `sameOrigin`), shutdown page; tests | BAT-FR-6, 12, 17 |
| P1-9 | `cmd/ui`: wiring, signals, graceful shutdown; systemd unit | BAT-NFR-3, Q1 |
| P1-10 | Docs: `SYSTEM.md`, `PRD.md`, `README.md` | - |
| P1-11 | On the Pi: AC-1..10 checklist, dry-run discharge, set final thresholds | Q3, Q7 |

Phase 2:

| Task | Scope | Covers |
|---|---|---|
| P2-1 | `system/timesync.go`, `system/boot.go` (`BootID`); tests | BAT-H-FR-10, 11 |
| P2-2 | `history/store.go`: sample format, buffered flush, retention, low disk; tests | BAT-H-FR-2..5, BAT-H-NFR-1, 2 |
| P2-3 | `history/session.go`: detector, resume; tests | BAT-H-FR-6..8, 9a |
| P2-4 | `history/offline.go`: inferred charge; tests | BAT-H-FR-9 |
| P2-5 | `history/recorder.go`: `Sink` + `Flusher`, shutdown reason passing; wiring | BAT-H-FR-1, 2 |
| P2-6 | `history/stats.go`; tests | BAT-H-FR-12, BAT-H-NFR-3 |
| P2-7 | `web`: `/battery`, SVG chart, downloads, reset; tests | BAT-H-FR-12..15 |
| P2-8 | Docs and on-Pi AC-H checklist | - |

Dependencies: P1-1 -> P1-2; P1-4 + P1-5 -> P1-7; P1-3, P1-6, P1-7 -> P1-8 ->
P1-9. Phase 2 starts after P1-9.

## 8. Open Points for Implementation
- Exact `VERSION` value range to accept (datasheet lists `0x001x`; confirm on
  the real chip during P1-2).
- `BATTERY_HISTORY_SAMPLE_INTERVAL` must be a multiple of the poll interval;
  if not, it is rounded up and a warning is logged.
- How the shutdown reason reaches the recorder's `Flush`: either a
  `FlushForShutdown(ctx, reason)` method on a separate interface, or the
  coordinator sets it on the recorder first. Decide in P2-5.
