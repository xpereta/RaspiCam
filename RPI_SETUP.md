# Raspberry Pi Setup From Scratch

This guide takes you from a blank microSD card to a running RaspiCam
(MediaMTX camera stream + status UI) on a Raspberry Pi Zero 2 W with a
Pi Camera Module 3.

There are two ways to do the software part:

- **Automated** (recommended): flash the card, clone the repo, run
  `sudo ./scripts/setup-pi.sh`. Sections 1-6 and 7.
- **Manual**: the same steps by hand, for understanding or debugging.
  Section 8 lists exactly what the script does.

Versions used by the script (override with flags, see section 7):

| Component        | Version  | Source                                   |
|------------------|----------|------------------------------------------|
| Raspberry Pi OS  | Lite (32-bit), Trixie | Raspberry Pi Imager |
| Go               | 1.27.4   | https://go.dev/dl/                       |
| MediaMTX         | v1.21.3  | https://github.com/bluenviron/mediamtx/releases |

MediaMTX's Raspberry Pi camera support requires Raspberry Pi OS Trixie or
Bookworm, 32-bit or 64-bit. Imager currently lists Trixie as "Raspberry Pi
OS Lite (32-bit)" and Bookworm as "Raspberry Pi OS (Legacy, 32-bit) Lite";
either works.

Sources for every step are listed in section 12.

## 1. What You Need

- Raspberry Pi Zero 2 W (512 MB RAM, 2.4 GHz-only 802.11n WiFi).
- Raspberry Pi Camera Module 3 and a **Standard-Mini camera cable**. The
  camera uses the standard 15-pin connector and ships with a
  Standard-Standard cable attached; all Pi Zero models use the mini 22-pin
  connector, so that cable does not fit.
- microSD card. Raspberry Pi recommends at least 8 GB for Raspberry Pi OS
  Lite.
- 5.1 V micro-USB power supply. Raspberry Pi's recommended supply current
  for the Zero 2 W is 2 A; the camera adds about 200-250 mA.
- A computer with a microSD card reader (macOS, Windows or Linux).
- A 2.4 GHz WiFi network.

## 2. Connect the Camera

Follows Raspberry Pi's "Install a Raspberry Pi camera" guide. Cameras are
sensitive to static; earth yourself before handling them.

1. Shut down the Pi and disconnect power.
2. Swap the camera's cable: on the camera, the connector is on the side
   opposite the lens. Open the flap (pull it out until it stops, tilt it
   slightly), remove the Standard-Standard cable, insert the 15-pin end of
   the Standard-Mini cable with the **metallic contacts facing away from the
   flap and towards the camera board**, and push the flap back until it
   clicks.
3. On the Pi Zero 2 W, the camera connector is on the short edge opposite the
   SD card slot. Open its flap the same way and insert the 22-pin end with
   the **metallic contacts facing away from the flap**. Close the flap.
4. Make sure both ends are fully and evenly inserted, not angled.
5. If there is a protective film on the lens, peel it off.

## 3. Flash Raspberry Pi OS

Raspberry Pi OS is distributed as a compressed disk image (`.img.xz`), not an
ISO. Raspberry Pi Imager downloads and writes it, and can pre-configure the
hostname, user, WiFi and SSH so the Pi works headless (no screen or
keyboard).

1. Download and install Raspberry Pi Imager from
   https://www.raspberrypi.com/software/ (on macOS, the community-maintained
   Homebrew cask `brew install --cask raspberry-pi-imager` also installs it).
2. Insert the microSD card and open Imager.
3. In the **Device** tab, select **Raspberry Pi Zero 2 W**, then **Next**.
4. In the **OS** tab, select **Raspberry Pi OS (other)** ->
   **Raspberry Pi OS Lite (32-bit)**, then **Next**. Lite is the
   command-line-only version for headless setups. (64-bit Lite also works;
   the script detects it.)
5. In the **Storage** tab, select your microSD card, then **Next**. Check
   the size to be sure it is the right device: it will be erased.
6. The **Customisation** tab opens. Fill in each subtab and select **Next**:
   - **Hostname**: `raspicam`. The Pi announces it over mDNS, so other
     devices can reach it as `raspicam.local`.
   - **Localisation**: choose your capital city. This sets the time zone,
     keyboard layout and the WiFi regulatory domain.
   - **User**: a username and password of your choice (lowercase letters,
     numbers, underscores, hyphens). You need these to log in over SSH and
     for `sudo`.
   - **Wi-Fi**: your network's SSID and password.
   - **Remote Access**: turn on **Enable SSH**, then choose password or
     public-key authentication (paste or browse to your public key, for
     example `~/.ssh/id_ed25519.pub`).
   - **Raspberry Pi Connect**: not needed; leave it off.
7. In the **Writing** tab, review the summary, select **Write**, then
   **I understand, erase and write**. Let Imager verify the card, then
   select **Finish**.

To download the image yourself instead, get it from
https://www.raspberrypi.com/software/operating-systems/ and select
**Use Custom** in Imager's OS tab. Customisation is offered when the image
supports it. If you skip customisation, Raspberry Pi OS asks for the same
information at first boot, which needs a monitor and keyboard (the Zero has
a mini-HDMI port and a micro-USB port for devices, so you need adapters).

## 4. First Boot and SSH

1. Insert the card into the Pi and power it on. The first boot can take a
   few minutes.
2. From your computer:
   ```
   ping raspicam.local
   ssh <your-user>@raspicam.local
   ```
   If `.local` does not resolve on your network, find the Pi's IP address in
   your router's list of connected devices and use `ssh <your-user>@<ip>`.

## 5. Update the OS and Check the Camera

On the Pi:

```
sudo apt update
sudo apt full-upgrade -y
sudo reboot
```

Reconnect, then check the camera is detected:

```
rpicam-hello --list-cameras
```

Expected output includes a line like `0 : imx708 [4608x2592 ...]` (imx708 is
the Camera Module 3 sensor). The camera is auto-detected
(`camera_auto_detect=1` in `/boot/firmware/config.txt`, the default); no
`raspi-config` change is needed. If no camera is listed, see
Troubleshooting.

## 6. Clone the Repository

```
sudo apt install -y git
git clone https://github.com/xpereta/RaspiCam.git ~/RaspiCam
cd ~/RaspiCam
```

## 7. Run the Setup Script

```
sudo ./scripts/setup-pi.sh
```

It takes several minutes on a Pi Zero 2 W (most of it is the first Go build).
It is safe to re-run. When it finishes it prints the URLs:

- Status UI: `http://raspicam.local:8080/`
- WebRTC player (browser): `http://raspicam.local:8889/cam`
- HLS (browser): `http://raspicam.local:8888/cam`
- RTSP (VLC, ffplay): `rtsp://raspicam.local:8554/cam`

Options (`sudo ./scripts/setup-pi.sh --help`):

| Option | Default | Meaning |
|--------|---------|---------|
| `--user NAME` | the user that ran `sudo` | runs the UI, owns the MediaMTX config |
| `--go-version X.Y.Z` | 1.27.4 | Go toolchain version |
| `--mediamtx-version vX.Y.Z` | v1.21.3 | MediaMTX release |
| `--ui-addr ADDR` | `:8080` | UI listen address |
| `--skip-apt` | off | skip `apt-get update`/`install` |

Then reboot once to confirm everything comes back on its own:

```
sudo reboot
```

### Updating Later

```
cd ~/RaspiCam
git pull
sudo ./scripts/setup-pi.sh
```

The script rebuilds and reinstalls the UI from the current checkout and
restarts the services. It keeps your existing `mediamtx.yml`. To move to a
newer MediaMTX, pass `--mediamtx-version`; the script then refreshes
`mediamtx.yml.dist` with the new upstream defaults and warns if your config
no longer passes `mediamtx --validate-conf`, so you can compare the two.

## 8. Manual Setup (What the Script Does)

Run these on the Pi as your normal user (commands use `sudo` where needed).
Replace `pi` with your username. For 64-bit OS use the `arm64` downloads
instead of `armv6l` (Go) / `armv7` (MediaMTX).

### 8.1 Packages

```
sudo apt install -y --no-install-recommends ca-certificates curl git iw rpicam-apps-lite
```

- `iw`: WiFi link details in the UI.
- `rpicam-apps-lite` is preinstalled on Raspberry Pi OS Lite; it provides
  `rpicam-hello`.
- `vcgencmd` (temperature, volts, throttling) opens `/dev/vcio_gencmd`,
  which Raspberry Pi OS's udev rules give to the `video` group. Check with
  `groups`; if `video` is missing, run `sudo usermod -aG video pi`. Without
  `vcgencmd` the UI runs in degraded mode.

### 8.2 Go Toolchain

`go.mod` requires Go 1.22 or newer. Debian Bookworm's `golang` package is
Go 1.19 (too old); Trixie's is 1.24. Use the official tarball either way, as
described in https://go.dev/doc/install. For 32-bit ARM, go.dev only
publishes `armv6l` builds, which also run on the Zero 2 W's ARMv7 userland.

```
cd /tmp
curl -fsSLO https://dl.google.com/go/go1.27.4.linux-armv6l.tar.gz
# SHA-256 as listed on https://go.dev/dl/
echo "e50964c41c6e187375387939e2f380de7250eba44e2259dfeb75c540d9d585c3  go1.27.4.linux-armv6l.tar.gz" | sha256sum -c
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.27.4.linux-armv6l.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/golang.sh
source /etc/profile.d/golang.sh
go version
```

### 8.3 MediaMTX

MediaMTX's release binaries embed a standard libcamera, so nothing else is
needed for official Raspberry Pi cameras. On 32-bit OS use `linux_armv7`
(both 32-bit ARM builds include Pi camera support; `linux_armv6` is for
ARMv6 boards such as the original Zero).

```
cd /tmp
V=v1.21.3
T=mediamtx_${V}_linux_armv7.tar.gz
curl -fsSLO https://github.com/bluenviron/mediamtx/releases/download/$V/$T
curl -fsSLO https://github.com/bluenviron/mediamtx/releases/download/$V/checksums.sha256
grep "$T" checksums.sha256 | sed 's/ \*/  /' | sha256sum -c
mkdir -p mediamtx && tar -C mediamtx -xzf $T
sudo install -m 0755 mediamtx/mediamtx /usr/local/bin/mediamtx
mediamtx --version
```

### 8.4 MediaMTX Config

As in MediaMTX's own start-on-boot guide, the config lives in its own
directory, `/usr/local/etc/mediamtx/`. Here it is owned by the UI user:

```
sudo install -d -m 0755 -o pi -g pi /usr/local/etc/mediamtx
cp /tmp/mediamtx/mediamtx.yml /usr/local/etc/mediamtx/mediamtx.yml
```

Why a directory and not `/usr/local/etc/mediamtx.yml`: the UI saves changes
by writing a temporary file next to the config, keeping a timestamped
`mediamtx.yml.bak-*` backup, and renaming the new file into place. That
needs write access to the directory. A dedicated directory avoids making
all of `/usr/local/etc` writable.

Edit `/usr/local/etc/mediamtx/mediamtx.yml`:

1. Enable the Control API on localhost only (the UI reads stream state from
   it):
   ```yaml
   api: true
   apiAddress: 127.0.0.1:9997
   ```
2. Replace everything inside the `paths:` section (the last section of the
   file), as MediaMTX's Raspberry Pi camera guide describes:
   ```yaml
   paths:
     cam:
       source: rpiCamera
   ```
   The path must be named `cam`; the UI edits the `rpiCamera*` keys under
   `paths.cam`.

`127.0.0.1` is stricter than the default `:9997`; the default
`authInternalUsers` already only allows API access from `127.0.0.1` and
`::1`.

Check the result:

```
mediamtx --validate-conf /usr/local/etc/mediamtx/mediamtx.yml
```

### 8.5 Build and Install the UI

```
cd ~/RaspiCam
GOFLAGS=-p=1 go build -trimpath -ldflags "-s -w" -o raspicam-ui ./cmd/ui
sudo install -m 0755 raspicam-ui /usr/local/bin/raspicam-ui
rm raspicam-ui
```

`-p=1` sets the number of build programs run in parallel to one (the
default is the number of CPUs), which lowers peak memory use on the 512 MB
Zero.

Alternative: cross-compile on your computer and copy the binary over:

```
GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "-s -w" -o raspicam-ui ./cmd/ui
scp raspicam-ui pi@raspicam.local:/tmp/
ssh -t pi@raspicam.local sudo install -m 0755 /tmp/raspicam-ui /usr/local/bin/raspicam-ui
```

`-t` gives `sudo` a terminal to ask for your password. Use `GOARCH=arm64`
and drop `GOARM` for a 64-bit OS.

### 8.6 systemd Services

```
cd ~/RaspiCam
sudo install -m 0644 systemd/mediamtx.service /etc/systemd/system/
sudo install -m 0644 systemd/raspicam-ui.service /etc/systemd/system/
sudo mkdir -p /etc/systemd/system/raspicam-ui.service.d
sudo tee /etc/systemd/system/raspicam-ui.service.d/override.conf > /dev/null << EOF
[Unit]
After=mediamtx.service

[Service]
User=pi
Group=pi
Environment=UI_ADDR=:8080
Environment=MEDIAMTX_API_URL=http://127.0.0.1:9997
Environment=MEDIAMTX_PATH_NAME=cam
Environment=MEDIAMTX_CONFIG_PATH=/usr/local/etc/mediamtx/mediamtx.yml
EOF
```

- `mediamtx.service` follows MediaMTX's start-on-boot guide (no `User=`, so
  it runs as root; `WorkingDirectory=/usr/local/etc/mediamtx`) and also
  passes the config path explicitly.
- The drop-in sets the UI user and points it at the config. Drop-ins update
  a unit without editing it; `User=` replaces the unit's value and
  `Environment=` lines add variables. The repo unit defaults to `User=pi`.
- Security note: MediaMTX runs as root but reads a config the UI user can
  write, and MediaMTX config can run commands (`runOnInit`, `runOnReady`,
  ...). So anyone who can run commands as that user can run commands as
  root. Raspberry Pi OS asks for the user's password for `sudo` by default,
  so this bypasses that password. The web UI itself only writes the
  validated `rpiCamera*` keys.

Boot ordering (see `SYSTEM.md`): MediaMTX's guide enables
`systemd-networkd-wait-online.service`, but Raspberry Pi OS configures the
network with NetworkManager (`nmcli`). systemd's documentation says the
matching wait service must be used, and `systemd-networkd-wait-online`
times out after 120 seconds by default when it has nothing to wait for:

```
sudo systemctl enable NetworkManager-wait-online.service
sudo systemctl disable systemd-networkd-wait-online.service   # if enabled
```

Enable and start:

```
sudo systemctl daemon-reload
sudo systemctl enable --now mediamtx raspicam-ui
```

## 9. Verify

```
systemctl status mediamtx raspicam-ui
curl -s http://127.0.0.1:9997/v3/paths/get/cam      # "ready": true
journalctl -u mediamtx -u raspicam-ui -f
```

Open `http://raspicam.local:8080/` for the status UI and
`http://raspicam.local:8889/cam` to watch the stream (HLS:
`http://raspicam.local:8888/cam`, RTSP: `rtsp://raspicam.local:8554/cam`).
After a reboot, check that both services are `active` again; look for boot
delays with `systemd-analyze blame | head`.

## 10. Troubleshooting

- **No camera detected** (`rpicam-hello --list-cameras` lists nothing):
  power off and check that both ends of the cable are firmly and evenly
  inserted with the contacts facing the right way (section 2), and that it
  is a Standard-Mini cable. Check `/boot/firmware/config.txt` still has
  `camera_auto_detect=1`. Update the OS (section 5). Try a different power
  supply.
- **MediaMTX cannot open the camera, or `rpicam-hello` fails while MediaMTX
  runs**: libcamera gives one process exclusive access to a camera. Stop the
  other user (`sudo systemctl stop mediamtx` to test with `rpicam-hello`),
  then `sudo systemctl start mediamtx`.
- **WebRTC page loads but no video**: MediaMTX's WebRTC guide says to add
  the server's LAN address to `webrtcAdditionalHosts` in `mediamtx.yml`, for
  example `webrtcAdditionalHosts: [192.168.1.50]`. HLS and RTSP do not need
  this.
- **UI shows "save error" for camera config**: the UI user cannot write
  `/usr/local/etc/mediamtx/`. Fix with
  `sudo chown -R pi:pi /usr/local/etc/mediamtx`.
- **UI shows no temperature/volts**: `vcgencmd` is missing or the user is not
  in the `video` group (`groups pi`). Re-login or restart the service after
  adding the group.
- **MediaMTX status "unknown" in the UI**: check `api: true` and
  `apiAddress: 127.0.0.1:9997` in the config, and that
  `curl http://127.0.0.1:9997/v3/paths/list` works on the Pi.
- **Go build killed / very slow**: likely out of memory. Make sure
  `GOFLAGS=-p=1` is set (the script does this), close other programs, or
  cross-compile (section 8.5).
- **Boot takes ~2 extra minutes**: `systemd-networkd-wait-online.service` is
  enabled; disable it (section 8.6).
- **Undervoltage / throttling in the UI**: use a supply that meets the
  recommended rating (section 1); try a different one.

## 11. Files Installed

| Path | Purpose |
|------|---------|
| `/usr/local/go/` | Go toolchain |
| `/etc/profile.d/golang.sh` | adds Go to `PATH` for login shells |
| `/usr/local/bin/mediamtx` | MediaMTX server |
| `/usr/local/etc/mediamtx/mediamtx.yml` | MediaMTX config (edited by the UI) |
| `/usr/local/etc/mediamtx/mediamtx.yml.dist` | pristine upstream config (script only) |
| `/usr/local/bin/raspicam-ui` | status UI binary |
| `/etc/systemd/system/mediamtx.service` | MediaMTX unit (from `systemd/`) |
| `/etc/systemd/system/raspicam-ui.service` | UI unit (from `systemd/`) |
| `/etc/systemd/system/raspicam-ui.service.d/override.conf` | UI user and env |

## 12. Sources

Each step was checked against these (versions as of November 2026):

- Raspberry Pi documentation (https://www.raspberrypi.com/documentation/,
  source https://github.com/raspberrypi/documentation):
  - Getting started: Install using Imager (tabs, customisation subtabs,
    writing); boot media sizes; configuration on first boot.
  - Raspberry Pi Zero: connectors, 2.4 GHz WiFi, 512 MB RAM.
  - Power supplies: Zero 2 W recommended current.
  - Camera: Install a Raspberry Pi camera (cables, orientation); hardware
    specification (IMX708, 4608x2592); rpicam-apps packages;
    `--list-cameras` output; troubleshooting.
  - config.txt: `camera_auto_detect`, `kernel`, `arm_64bit` defaults.
  - Find your IP address (mDNS `.local`); Updating (`apt full-upgrade`);
    Users (sudo asks for a password by default); Networking (`nmcli`).
- Imager OS list: https://downloads.raspberrypi.com/os_list_imagingutility_v4.json
- Raspberry Pi OS udev rules (package `raspberrypi-sys-mods`):
  `vcio_gencmd` and `dma_heap` belong to group `video`.
- Go: https://go.dev/doc/install, https://go.dev/dl/, `go help build`
  (`-p`), `go help environment` (`GOFLAGS`, `GOARM`).
- Debian: golang-defaults 1.19 (Bookworm) / 1.24 (Trixie);
  `/etc/profile` sources `/etc/profile.d/*.sh` (base-files).
- MediaMTX v1.21.3 (https://github.com/bluenviron/mediamtx/tree/v1.21.3):
  docs Install, Start on boot, Raspberry Pi Cameras, Configuration (hot
  reload), Control API, Authentication, WebRTC/HLS/RTSP reading, WebRTC
  specific features; `mediamtx.yml` defaults; `api/openapi.yaml`
  (`/v3/paths/get/{name}`, `ready`); `scripts/binaries.mk` (armv6/armv7
  builds); `--version` / `--validate-conf` in `internal/core/core.go`;
  config watcher in `internal/confwatcher`.
- libcamera: `Camera::acquire()` is exclusive (`-EBUSY` for other
  processes; listing still works).
- systemd: `NETWORK_ONLINE.md`, `systemd.unit(5)` drop-ins,
  `systemd.exec(5)` `Environment=`, `systemd-networkd-wait-online`
  `--timeout` default.
