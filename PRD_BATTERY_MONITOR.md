# PRD: Battery Monitoring and Safe Shutdown (MAX17048)

## Status
Draft. Two phases:
- Phase 1: live battery readings and low-battery safe shutdown.
- Phase 2: battery history and charge/discharge statistics (see
  "Phase 2: Battery History and Statistics").

Spec-driven development: this PRD defines *what* and *why*. A follow-up
design spec (`SPEC_BATTERY_MONITOR.md`) will define *how*, and a task list will
be derived from it. Requirement IDs (`BAT-*`) must be referenced from the spec,
tasks, tests, and PRs for traceability.

## Overview
The Pi Zero 2 W is powered from two 18650 Li-ion cells in parallel (1S2P) through
a boost/regulator module that outputs 5V to the Pi. Today the UI only sees the
5V side indirectly (`vcgencmd get_throttled` under-voltage flags) and has no
visibility into the battery itself. When the pack runs flat the Pi loses power
abruptly, risking SD card corruption and truncated recordings.

Add a MAX17048 fuel gauge on the battery pack, read it over I2C, show battery
voltage and state of charge in the UI, and shut the Pi down cleanly before the
pack is exhausted.

## Background: What the MAX17048 Measures
- Single-cell (1S) Li-ion/LiPo fuel gauge. Two cells in parallel are electrically
  one cell with double capacity, so 1S2P is supported.
- It measures the **battery cell voltage (approx. 3.0V-4.2V)**, not the 5V
  output of the regulator. It must be wired across the battery pack (before the
  boost module). The 5V rail stays covered by the existing `vcgencmd` metrics.
- It computes state of charge (SOC, %) internally with Maxim's ModelGauge
  algorithm. No sense resistor and no capacity configuration are needed. SOC is
  read from the chip, not calculated by us.
- It also reports an approximate charge/discharge rate (`CRATE`, %/hour), which
  allows a rough time-to-empty estimate.
- I2C address `0x36`, 16-bit big-endian registers. Relevant registers:
  - `VCELL` (0x02): cell voltage, 78.125 uV/LSB.
  - `SOC` (0x04): state of charge, 1/256 %/LSB.
  - `VERSION` (0x08): chip version, used for detection.
  - `CRATE` (0x16): signed, 0.208 %/hour/LSB.
  - `STATUS` (0x1A): reset and alert flags.
- Datasheet: https://www.analog.com/en/products/max17048.html

## Goals
- G1: Show battery voltage in the UI.
- G2: Show remaining battery percentage in the UI.
- G3: Shut the Pi down safely and automatically on low battery.
- Keep resource usage negligible on the Pi Zero 2 W.
- Behave sensibly when the gauge is absent (e.g. mains-powered setups, dev
  machines).

## Non-Goals
- Charging control or charger status (the charging module is not monitored).
- Measuring current or power in mA/mW (the MAX17048 cannot).
- Monitoring the 5V output rail beyond what `vcgencmd` already provides.
- Using the MAX17048 `ALRT` interrupt pin (polling only, for now).
- Automatic power-on when power is restored (hardware concern).
- Historical charts or long-term battery logging in phase 1 (covered by
  phase 2).
- Notifications (email, push, etc.).

## User Stories
- As an operator, I open the UI and see the battery voltage and percentage so
  I know how long the camera can keep running.
- As an operator, I see whether the battery is charging or discharging and a
  rough time remaining.
- As an operator, I can trust the Pi to power off cleanly before the batteries
  die, so the SD card and the last recording are not corrupted.
- As an operator, I can see in the UI and the journal that a low-battery
  shutdown is configured, and why the Pi shut down after it happens.
- As a developer, I can run the UI on a machine without the gauge and the page
  still works, showing the battery as "not detected".

## Functional Requirements

### Gauge Access
- BAT-FR-1: Read the MAX17048 over Linux I2C (`/dev/i2c-1` by default, address
  `0x36` by default). Both are configurable.
- BAT-FR-2: Detect the gauge at startup by reading `VERSION`. If the device or
  bus is missing, the feature runs in degraded mode (see BAT-FR-12) and does not
  fail the UI.
- BAT-FR-3: Decode `VCELL`, `SOC`, and `CRATE` with strict, unit-tested parsing
  functions (raw register bytes -> volts, percent, %/hour).
- BAT-FR-4: Clamp displayed SOC to 0-100% (the chip can report slightly above
  100% when full).
- BAT-FR-5: All I2C reads are bounded by a timeout and are safe for concurrent
  access (a single owner of the bus handle).

### UI
- BAT-FR-6: Add a "Battery" card to the status page showing:
  - Battery voltage (V, 2 decimals).
  - State of charge (%, integer) with a simple level indicator.
  - Trend: charging / discharging / idle, from `CRATE` sign and magnitude.
  - Estimated time remaining while discharging (`SOC / |CRATE|`), shown as
    approximate (e.g. "~3h 20m"); hidden when not discharging or unreliable.
  - Level status: `OK`, `Low`, `Critical`.
  - The configured shutdown threshold(s) and whether auto-shutdown is enabled.
  - Time of the last successful reading.
- BAT-FR-7: Values are read when the page is rendered (no auto-refresh),
  consistent with the rest of the UI. The page may reuse the latest reading from
  the background monitor (BAT-FR-8) if it is fresh, to avoid extra bus traffic.

### Low-Battery Monitoring and Shutdown
- BAT-FR-8: A lightweight background monitor polls the gauge at a fixed
  interval (default 30s) independently of page views. This is an intentional,
  documented exception to "no background polling": shutdown cannot depend on
  someone having the UI open.
- BAT-FR-9: Shutdown triggers when, while discharging, SOC is at or below the
  shutdown SOC threshold (default 5%) **or** voltage is at or below the shutdown
  voltage threshold (default 3.30V). The voltage threshold is a safety net in
  case the SOC model is inaccurate (e.g. after a cold start or aging cells).
- BAT-FR-10: Avoid false triggers:
  - Require N consecutive low readings (default 3) before acting.
  - Ignore readings during a startup grace period (default 2 minutes) while
    ModelGauge settles after power-up.
  - Discard implausible readings (e.g. voltage outside 2.5V-4.5V, failed reads);
    a read error never counts as a low reading.
  - Do not shut down while the battery is charging.
- BAT-FR-11: When triggered, the monitor:
  - Logs a clear message to the journal with voltage, SOC, and the threshold
    that was hit.
  - Appends a record to the shutdown log (BAT-FR-18) so the UI can show it
    after the next boot.
  - Requests a clean OS shutdown (`systemctl poweroff`), letting systemd stop
    MediaMTX so the current recording segment is finalized.
- BAT-FR-12: Degraded mode: if the gauge is not detected or reads fail
  repeatedly, the Battery card shows "Not detected" / "Read error" with the
  reason, and auto-shutdown is reported as inactive. A gauge that disappears
  never triggers a shutdown.
- BAT-FR-13: A "Low" warning level (default 20% SOC) is shown in the UI only;
  it does not trigger any action.
- BAT-FR-14: A dry-run mode logs (and shows in the UI) that a shutdown would
  have happened without powering off. Used for bench testing and calibration.

### Manual Power Off
- BAT-FR-17: Add a "Power off" button to the UI (in the Battery card) so the
  operator can shut the Pi down cleanly before switching off power or charging.
  - The button submits a POST form; a GET never triggers a shutdown.
  - A confirmation step is required (e.g. a confirmation page or a confirm
    checkbox in the form), so it cannot be triggered by a single accidental
    click.
  - It uses the same shutdown path as BAT-FR-11, with reason `manual`: log the
    request (including the client address), append to the shutdown log
    (BAT-FR-18), flush phase 2 history when enabled, then `systemctl poweroff`.
  - After a confirmed request, the UI shows a "Shutting down, wait for the
    activity LED to stop before cutting power" page.
  - The button works even when the gauge is not detected (it does not depend
    on battery readings).
  - In dry-run mode (BAT-FR-14) the request is logged and shown but the Pi
    does not power off.
  - If the service lacks permission to power off, the button is shown disabled
    with the reason.

### Shutdown Log
- BAT-FR-18: Keep a log of the last 10 shutdowns triggered by the UI (low
  battery or manual).
  - Each record has: time, reason (`low_battery`, `manual`), threshold hit
    (for `low_battery`), voltage, SOC, and whether wall time was synchronized.
  - Stored as JSON lines in the state directory (default
    `/var/lib/raspicam/shutdowns.jsonl`, via systemd `StateDirectory=`).
    Older records beyond 10 are dropped when a new one is written.
  - Written and synced to disk before `systemctl poweroff` is called.
  - The Battery card shows the most recent shutdown; the full list (newest
    first) is shown below it or on the phase 2 `/battery` page.
  - Corrupt or truncated lines are skipped with a log message.
  - Phase 2 sessions reference these records for their end reason instead of
    duplicating them.

### Configuration
- BAT-FR-15: Configure via environment variables, consistent with existing
  settings (exact names finalized in the spec):
  - `BATTERY_ENABLED` (default `true`; `false` disables gauge and monitor).
  - `BATTERY_I2C_DEVICE` (default `/dev/i2c-1`).
  - `BATTERY_I2C_ADDR` (default `0x36`).
  - `BATTERY_POLL_INTERVAL` (default `30s`).
  - `BATTERY_LOW_SOC` (default `20`).
  - `BATTERY_SHUTDOWN_SOC` (default `5`).
  - `BATTERY_SHUTDOWN_VOLTS` (default `3.30`).
  - `BATTERY_SHUTDOWN_SAMPLES` (default `3`).
  - `BATTERY_SHUTDOWN_DRY_RUN` (default `false`).
- BAT-FR-16: Validate configuration at startup (ranges, `SHUTDOWN_SOC <
  LOW_SOC`, sane voltage). Invalid values are logged and replaced by defaults
  rather than crashing the UI.

## Non-Functional Requirements
- BAT-NFR-1: Standard library only. I2C access uses `/dev/i2c-*` with the
  `I2C_SLAVE` ioctl via `syscall`; adding `golang.org/x/sys` requires
  justification in the spec.
- BAT-NFR-2: Negligible CPU and memory: one small read every poll interval, no
  allocations in the steady-state polling loop beyond trivial ones.
- BAT-NFR-7: Monitoring must not measurably increase power consumption.
  - Gauge side: the MAX17048 draws about 23uA active and about 4uA in
    hibernate, from the pack directly. Keep the chip's automatic hibernate
    enabled (do not force it awake). This is under 0.05% of the Pi's typical
    100-250mA draw.
  - Pi side: each poll is two short I2C reads (VCELL+SOC, then CRATE)
    lasting well under 1ms in total. No subprocesses (no `i2cget`/`vcgencmd` per poll),
    no busy-waits or sleeps in loops, a single `time.Ticker` for the monitor.
  - Keep the I2C device handle open between polls rather than reopening it.
  - Default poll interval of 30s; a minimum of 10s is enforced so it cannot be
    configured into a power-hungry tight loop. Shutdown latency is bounded by
    `poll interval x shutdown samples` (default 90s), acceptable given the
    discharge rate of a 2x18650 pack.
  - Page renders reuse the monitor's latest reading when it is fresh (within
    one poll interval) instead of issuing extra reads.
  - The monitor never prevents the CPU from idling (no timers faster than the
    poll interval, no polling of the `ALRT` pin).
- BAT-NFR-8: The monitor publishes each reading to consumers (UI cache, and the
  phase 2 recorder) through a small interface, so phase 2 adds no extra bus
  reads.
- BAT-NFR-3: The monitor uses `context.Context` and stops cleanly when the UI
  process exits.
- BAT-NFR-4: The shutdown path is testable without hardware: gauge reader and
  shutdown action are interfaces injected via constructors (no globals).
- BAT-NFR-5: Unit tests cover register decoding, threshold/hysteresis logic,
  grace period, charging suppression, error handling, and config validation.
  No tests require Pi hardware.
- BAT-NFR-6: Fail safe: any error in the battery feature must not break the
  status page or other cards.

## Phase 2: Battery History and Statistics

### Goal
Track how long the battery takes to discharge (primary) and to charge
(secondary), and show statistics in the UI. Phase 2 depends on phase 1 and does
not change its shutdown behavior.

### Key Concepts
- Sample: one reading (time, voltage, SOC, CRATE) taken from the phase 1
  monitor.
- Session: a continuous period in one state, `discharging` or `charging`, with a
  start, an end, and an end reason. Sessions are the main unit for statistics.
- Full discharge cycle: a discharging session that starts at or above a "full"
  SOC (default 95%) and ends with a low-battery shutdown. Runtime statistics
  are computed only from full cycles, so partial sessions do not skew them.

### Functional Requirements

#### Recording
- BAT-H-FR-1: Record samples from the phase 1 monitor at a configurable
  interval (default 60s; samples may be decimated from the 30s poll). No extra
  I2C reads.
- BAT-H-FR-2: Buffer samples in memory and append them to disk in batches
  (default every 15 minutes), on session change, on clean exit, and
  immediately before a low-battery shutdown. A crash loses at most one batch.
- BAT-H-FR-3: Store data under a state directory (default
  `/var/lib/raspicam/battery`, via systemd `StateDirectory=`), using
  append-only, human-readable files (CSV for samples, JSON lines for sessions).
  Standard library only; no database.
- BAT-H-FR-4: Retention is size-based: raw samples are stored in daily files,
  and when their total size exceeds a configurable limit (default 10 MB,
  about 7 months at 60s samples) the oldest daily files are deleted until it
  fits. The `/battery` page shows how far back samples go. Session records and
  the shutdown log are kept indefinitely and do not count toward the limit
  (they are small).
- BAT-H-FR-5: Tolerate corrupt or truncated lines (e.g. after power loss) by
  skipping them with a log message; never fail the UI because of history data.

#### Session Detection
- BAT-H-FR-6: Detect state changes (discharging / charging / idle) from CRATE
  and SOC trend, with hysteresis so noise does not create spurious sessions
  (e.g. require the new state for several consecutive samples).
- BAT-H-FR-7: Each session records: state, start/end time, start/end SOC,
  start/end voltage, duration, min voltage, average rate (%/hour), and end
  reason (`low_battery_shutdown`, `charger_connected`, `charger_removed`,
  `full`, `reboot`, `unknown`).
- BAT-H-FR-8: Persist the open session so a reboot or UI restart resumes it
  instead of splitting it in two. If the gap since the last sample is longer
  than a threshold (default 10 minutes), close the old session with reason
  `unknown` and start a new one.
- BAT-H-FR-9: Charging while the Pi is off (the normal case, see Q8): on boot,
  if SOC rose significantly since the last recorded sample, record an "off-line
  charge" event. Its primary data is charge added (start SOC -> end SOC) and
  whether the pack reached full. The elapsed wall time is stored only as a
  rough upper bound on duration and labeled as inferred, not measured.
- BAT-H-FR-9a: Charging while the Pi is running (optional, supported by the UPS
  charge module): detected automatically from CRATE, with no operator action.
  Recorded as a normal measured `charging` session with duration and curve.
  Labeled "charged under load", since the Pi's draw lengthens charge time.

#### Time Handling
- BAT-H-FR-10: Measure durations within a boot with the monotonic clock, so
  they stay correct even if wall time changes.
- BAT-H-FR-11: The Pi Zero 2 W has no RTC. Record whether wall time was
  synchronized (NTP) for each session. Durations spanning a reboot with an
  unsynchronized clock are marked as unreliable and excluded from averages.

#### UI: Battery Statistics Section
- BAT-H-FR-12: Add a separate `/battery` page (Q9), linked from the Battery
  card, showing:
  - Last full discharge: runtime (full to shutdown), start/end SOC and voltage.
  - Average and most recent runtime over the last N full cycles (default 5).
  - Average discharge rate (%/hour) and average charge rate when measured.
  - Last charge: charge added and whether it reached full; duration when
    measured, or the inferred upper bound clearly labeled.
  - Average charge time is computed only from measured charging sessions;
    inferred durations are never averaged.
  - Runtime per full cycle over time, to show capacity loss as cells age.
  - Table of recent sessions (newest first, limited to e.g. 20).
- BAT-H-FR-13: Show a small voltage and SOC chart for the current or last
  session, rendered server-side as inline SVG from Go templates. No JavaScript
  libraries and no auto-refresh.
- BAT-H-FR-14: Offer downloads of raw samples (CSV) and sessions (JSON lines)
  for analysis elsewhere.
- BAT-H-FR-15: Allow resetting history (e.g. after replacing the cells), with
  a confirmation step. Reset archives the old files rather than deleting them.

#### Configuration
- BAT-H-FR-16: Environment variables (names finalized in the spec):
  - `BATTERY_HISTORY_ENABLED` (default `true` when the gauge is detected).
  - `BATTERY_HISTORY_DIR` (default `/var/lib/raspicam/battery`).
  - `BATTERY_HISTORY_SAMPLE_INTERVAL` (default `60s`, minimum `30s`).
  - `BATTERY_HISTORY_FLUSH_INTERVAL` (default `15m`).
  - `BATTERY_HISTORY_MAX_SIZE` (default `10MB`).
  - `BATTERY_FULL_SOC` (default `95`).

### Non-Functional Requirements
- BAT-H-NFR-1: Power and SD wear: no more than one batched write per flush
  interval in steady state (about 100 writes/day by default). Power use stays
  within BAT-NFR-7 (AC-9 still applies with history enabled).
- BAT-H-NFR-2: Disk usage: raw samples never exceed the size limit (default
  10 MB) by more than one daily file. If free space
  drops below a threshold (e.g. 100 MB), stop recording samples and show a
  warning; recordings from MediaMTX take priority.
- BAT-H-NFR-3: Memory: the in-memory buffer and statistics stay under 1 MB.
  Statistics are computed from session records, not by scanning all samples
  on every page load.
- BAT-H-NFR-4: Unit tests cover session detection (state changes, hysteresis,
  resume after reboot, gap handling, off-line charge inference), file parsing
  with corrupt lines, retention, and statistics calculations. No hardware
  needed.

### Phase 2 Risks and Mitigations
- Risk: Charging happens while the Pi is off, so charge time cannot be
  measured.
  - Mitigation: record charge added and full/not full, which are accurate
    because the gauge stays powered from the pack (BAT-H-FR-9). Measured charge
    time is available whenever the Pi runs while charging (BAT-H-FR-9a).
- Risk: Removing the cells to charge them externally resets the gauge, and its
  first SOC readings after reset are a rough voltage-based estimate.
  - Mitigation: document charging in place; mark the first session after a
    gauge reset (`STATUS.RI` flag) as low confidence.
- Risk: Wrong wall time after boot without network.
  - Mitigation: monotonic durations, sync flag per session (BAT-H-FR-10/11).
- Risk: Power loss corrupts the last lines of a file.
  - Mitigation: append-only files, tolerant parsing, flush before shutdown.
- Risk: Noisy CRATE creates many tiny sessions.
  - Mitigation: state hysteresis and a minimum session duration.

### Phase 2 Open Questions
- Q8 (resolved): The charge module works as a UPS (load sharing), so the Pi
  can run while charging. In practice the train is normally charged with the
  Pi off. Decision: design for charge-while-off as the default (BAT-H-FR-9) and
  measure charging automatically whenever the Pi happens to be running
  (BAT-H-FR-9a).
- Q9 (resolved): Separate `/battery` page with statistics, charts, session
  table, shutdown log, and downloads. The status page keeps the compact
  Battery card (voltage, SOC, status, power-off button, last shutdown) with a
  link to `/battery`. History files are read only when `/battery` is opened.
- Q10 (resolved): Size-based retention for raw samples, default 10 MB (about
  7 months), oldest daily files deleted first (BAT-H-FR-4).
- Q11 (resolved): No RTC module for now. Rely on `fake-hwclock` (default on
  Raspberry Pi OS) and network time sync; records made before sync are marked
  unreliable (BAT-H-FR-11). Revisit with a DS3231 on the shared I2C bus if
  timestamps prove to be a problem.

### Phase 2 Acceptance Criteria
- AC-H-1 (BAT-H-FR-7, BAT-H-FR-12): After a full discharge from 100% to
  low-battery shutdown, the next boot shows a session with runtime within 2
  minutes of the actual time measured by the operator.
- AC-H-2 (BAT-H-FR-8): Restarting the UI during a discharge does not split the
  session.
- AC-H-3 (BAT-H-FR-9): Charging with the Pi off produces one inferred charge
  record with start/end SOC and an upper-bound duration, labeled as inferred.
- AC-H-3a (BAT-H-FR-9a): Connecting the charger with the Pi running produces a
  measured charging session ending with reason `full` or `charger_removed`,
  and no low-battery shutdown happens while charging.
- AC-H-4 (BAT-H-FR-2, BAT-H-NFR-1): With default settings, the history files
  are written at most once per flush interval in steady state, and data up to
  the shutdown is present after a low-battery shutdown.
- AC-H-5 (BAT-H-FR-5): A file with a truncated last line loads, skips that
  line, and the page renders.
- AC-H-6 (BAT-H-FR-4, BAT-H-NFR-2): After simulating more samples than fit
  in the limit, raw sample files total at most 10 MB plus one daily file, the
  oldest days are the ones removed, and sessions and the shutdown log are
  untouched.

## Future Improvements
Not part of phase 1 or phase 2. Listed so the design leaves room for them.

- FUT-1: Physical power-off button. The UI button is in phase 1 (BAT-FR-17).
  A physical push button on a GPIO pin would allow a clean shutdown without a
  phone or laptop, using the stock `gpio-shutdown` device tree overlay (no
  code needed). The UI flushes phase 2 history through its normal SIGTERM
  handling when systemd stops it.

## Hardware and OS Setup (to document in `SYSTEM.md`)
- Wiring (MAX17048 breakout):
  - Battery pack `+`/`-` to the gauge `CELL+`/`CELL-` (or JST battery input),
    upstream of the 5V boost module.
  - `SDA` -> GPIO2 (pin 3), `SCL` -> GPIO3 (pin 5), `GND` -> Pi GND (pin 6).
  - Confirm the breakout's I2C pull-ups/logic level are 3.3V compatible.
- Enable I2C: `sudo raspi-config` -> Interface Options -> I2C (adds
  `dtparam=i2c_arm=on`).
- Verify: `sudo apt install i2c-tools` then `i2cdetect -y 1` shows `36`.
- Permissions: the UI service user must be in the `i2c` group and be allowed to
  power off the system (polkit rule or a narrowly scoped sudoers entry for
  `systemctl poweroff`). Running the UI as root is not required.

## Power Hardware Notes
- Module: generic "UPS-15W" 18650 charger/boost module, 5V output (AliExpress
  item 1005010438773914). Seller specs:
  - UPS function: charger input powers the load and charges the cells; on
    charger removal it switches to the cells via the boost circuit.
  - Boost: 15W nominal (20W peak), 5V/3A output, up to 96% efficiency.
  - Quiescent draw: about 25mA; desoldering the output indicator LED saves
    more than 10mA.
- Recommendation: desolder the module's output indicator LED. Estimated
  effect (not yet measured on this setup):
  - Streaming (about 450-600mA from the cells): about 2% saving, roughly
    10-15 extra minutes per charge on a 5000-6000mAh pack.
  - Idle with WiFi (about 150-180mA from the cells): about 6%.
  - Halted after a low-battery shutdown (about 55-70mA including the module):
    about 15-20%, extending the time before the module's cutoff trips.
  - For comparison, the MAX17048 draws about 0.023mA.
  - Verify with a USB power meter as part of AC-9.
  - Cell protection: overcharge, over-discharge, over-temperature and
    over-current. The over-discharge cutoff voltage is **not documented**.
    After a protection trip the output stays off until a 5V charger is
    connected.
  - Seller note: if the charger has a large output capacitor, its voltage
    decays slowly on unplug and can delay the switch to battery; a 1K resistor
    across the 5V input fixes it.
- The charge module works as a UPS: it keeps 5V to the Pi while the charger is
  connected and switches to battery when it is removed.
- Usage: the Pi runs in a moving Lego train, on battery. The pack is normally
  charged in place with the Pi off; running while charging is possible but
  optional.
- Charge the cells in place: removing them resets the MAX17048.
- After a low-battery shutdown the Pi stays halted even if the charger is
  connected, because the UPS keeps 5V present and the Pi only boots on a power
  cycle. To restart, power-cycle the 5V output (switch) after connecting the
  charger. Exception: if the module's over-discharge protection has already
  cut the output, connecting the charger restores 5V and the Pi boots on its
  own.

## Constraints
- Raspberry Pi OS 32-bit on Pi Zero 2 W.
- MAX17048 supports 1S packs only; the 2x18650 must stay in parallel.
- The Pi cannot cut its own power: after `poweroff` the halted Pi and the boost
  module keep drawing a small current from the pack.

## Risks and Mitigations
- Risk: False shutdown from a noisy or wrong reading.
  - Mitigation: consecutive-sample hysteresis, plausibility checks, grace
    period, read errors never count as low.
- Risk: SOC inaccurate right after power-up or with aged cells.
  - Mitigation: voltage threshold as an independent trigger; dry-run mode for
    calibration; quick-start command left as an open question.
- Risk: Load spikes (WiFi + encoder) cause voltage sag and early shutdown.
  - Mitigation: N consecutive samples spaced by the poll interval; conservative
    default voltage threshold.
- Risk: Over-discharge after shutdown because the halted Pi and boost module
  still drain the pack.
  - Mitigation: use protected 18650 cells or a boost/BMS with low-voltage
    cutoff; document recommended thresholds; leave margin in default SOC.
- Risk: UI service lacks permission to power off.
  - Mitigation: check at startup (where possible), show "shutdown not
    permitted" in the Battery card, document polkit/sudoers setup.
- Risk: Anyone on the local network can power off the Pi with the UI button
  (there is no authentication).
  - Mitigation: POST plus confirmation step, request logged with client
    address; accepted for now given the local-network-only security posture.
- Risk: I2C bus conflicts with other devices.
  - Mitigation: single owner for the bus handle; default address `0x36` is
    configurable.

## Open Questions
- Q1 (resolved): The shutdown monitor runs inside `raspicam-ui`, with
  `Restart=always` in the systemd unit so a crash only pauses monitoring for a
  few seconds.
- Q2 (resolved): The gauge is read directly from userspace via `/dev/i2c-1`
  (standard library, `I2C_SLAVE` ioctl). The kernel `max17040` driver is not
  used: it needs a custom device tree overlay and may not expose `CRATE`.
- Q3 (resolved): Defaults are 5% SOC / 3.30V as placeholders. Final values
  are set after one full discharge in dry-run mode (BAT-FR-14) with the camera
  streaming, using the recorded voltage/SOC curve. Q7 may raise the voltage
  floor.
- Q4 (resolved): A UI power-off button is in scope for phase 1 (BAT-FR-17). A
  physical button is a future improvement (FUT-1).
- Q5 (resolved): The UI only reads from the gauge and leaves chip defaults: no
  quick-start, no `RCOMP` or alert-threshold writes. Revisit only if SOC proves
  inaccurate in testing.
- Q6 (resolved): Phase 1 keeps a log of the last 10 shutdowns (BAT-FR-18),
  not just the most recent one.
- Q7 (resolved): The module has over-discharge protection, but the cutoff
  voltage is not documented. Keep 3.30V for now; during the Q3 dry-run
  discharge, record the voltage at which the module cuts its output, then set
  the shutdown voltage with a margin above it (at least 0.15V for load sag).
  After a low-battery shutdown, the module (about 25mA) and the halted Pi keep
  draining the pack until the module's own cutoff trips; this is expected.

## Acceptance Criteria
- AC-1 (BAT-FR-6): With the gauge connected, the Battery card shows voltage
  within +/-0.05V of a multimeter reading at the battery terminals, and an SOC
  between 0 and 100%.
- AC-2 (BAT-FR-6): The card shows charging vs discharging correctly when the
  charger is connected/disconnected, and an approximate time remaining while
  discharging.
- AC-3 (BAT-FR-12): With the gauge disconnected or `BATTERY_ENABLED=false`, the
  page loads normally and the card shows "Not detected" / "Disabled"; no
  shutdown is ever triggered.
- AC-4 (BAT-FR-9, BAT-FR-10, BAT-FR-14): In dry-run mode, discharging past the
  threshold logs exactly one "would shut down" event after N consecutive low
  samples, and none during the grace period or while charging.
- AC-5 (BAT-FR-11): With dry-run off, reaching the threshold powers the Pi off
  cleanly: the journal shows the reason, MediaMTX stops gracefully, the last
  recording file is playable, and the filesystem is clean on next boot.
- AC-6 (BAT-FR-11, BAT-FR-18): After reboot, the UI shows the last shutdown
  time and reason. After 11 shutdowns (dry-run or real), the log holds exactly
  the 10 most recent, newest first.
- AC-10 (BAT-FR-17): Pressing "Power off" and confirming shuts the Pi down
  cleanly; the next boot shows the last shutdown reason as `manual`. Without
  confirmation, or via GET, nothing happens. In dry-run mode the request is
  only logged.
- AC-7 (BAT-NFR-5): Unit tests pass for register decoding and the shutdown
  decision logic without hardware.
- AC-8 (BAT-NFR-2): CPU usage of the UI process does not measurably increase
  with the monitor running.
- AC-9 (BAT-NFR-7): With the camera idle (MediaMTX stopped), input current to
  the Pi measured with a USB power meter differs by less than 1mA (within meter
  noise) between `BATTERY_ENABLED=false` and `BATTERY_ENABLED=true` over a
  10-minute average. A poll interval below 10s is rejected at startup.

## Documentation Updates (on implementation)
- `PRD.md`: add battery voltage/SOC to the UI "Shows" list and auto-shutdown.
- `SYSTEM.md`: wiring, I2C enablement, permissions, environment variables.
- `README.md`: new environment variables.
- `systemd/raspicam-ui.service`: battery env vars, `Restart=always`,
  `StateDirectory=raspicam` (phase 1 shutdown log, phase 2 history).
