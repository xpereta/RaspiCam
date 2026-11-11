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
| Raspberry Pi OS  | Lite 32-bit (Trixie or Bookworm) | Raspberry Pi Imager |
| Go               | 1.27.4   | https://go.dev/dl/                       |
| MediaMTX         | v1.21.3  | https://github.com/bluenviron/mediamtx/releases |

## 1. What You Need

- Raspberry Pi Zero 2 W.
- Raspberry Pi Camera Module 3 and a **Pi Zero camera cable** (22-pin on the
  Pi end, 15-pin on the camera end). The cable that ships with the camera is
  for full-size Pis and does not fit the Zero.
- microSD card, 8 GB or larger (Class 10 / A1 or better).
- 5 V micro-USB power supply, 2.5 A (the official Pi Zero supply is ideal;
  weak supplies cause undervoltage warnings in the UI).
- A computer with a microSD card reader (macOS, Windows or Linux).
- A 2.4 GHz WiFi network (the Zero 2 W has no 5 GHz radio).

## 2. Connect the Camera

Do this with the Pi powered off.

1. On the Pi Zero 2 W, gently pull the black latch of the camera connector
   (at the short edge of the board) outward.
2. Insert the narrow end of the cable with the **contacts facing the board**
   (towards the PCB), push the latch back in.
3. On the camera, lift the latch, insert the wide end with the contacts
   facing the camera PCB, close the latch.
4. Peel the protective film off the camera lens.

## 3. Flash Raspberry Pi OS

Raspberry Pi OS is distributed as a compressed disk image (`.img.xz`), not an
ISO. The easiest way to download and write it is Raspberry Pi Imager, which
also pre-configures WiFi, user and SSH so the Pi works headless.

1. Download and install Raspberry Pi Imager from
   https://www.raspberrypi.com/software/
   (macOS: `brew install --cask raspberry-pi-imager` also works).
2. Insert the microSD card and open Imager.
3. Choose:
   - **Device**: Raspberry Pi Zero 2 W.
   - **Operating system**: Raspberry Pi OS (other) ->
     **Raspberry Pi OS Lite (32-bit)**. Lite has no desktop, which saves RAM
     on the 512 MB Zero. (64-bit Lite also works; the script detects it.)
   - **Storage**: your microSD card. Everything on it will be erased.
4. When asked to apply OS customisation, edit the settings:
   - **Hostname**: `raspicam` (the Pi will be reachable as `raspicam.local`).
   - **Username and password**: pick your own, for example `pi`. There is no
     default user any more; remember what you choose.
   - **Wireless LAN**: SSID, password, and **wireless LAN country** (required,
     WiFi stays disabled without it).
   - **Locale**: your time zone and keyboard layout.
   - **Services**: enable **SSH**. Public-key authentication is recommended
     (paste the contents of `~/.ssh/id_ed25519.pub`); password auth is fine
     on a trusted LAN.
5. Write the card and wait for verification to finish. Eject it.

Alternative without Imager's customisation: download the image from
https://www.raspberrypi.com/software/operating-systems/, flash it with any
tool (Imager "Use custom", balenaEtcher, `dd`), then connect a keyboard and
screen on first boot to create the user and run `sudo raspi-config` to set
WiFi, hostname and SSH. The rest of this guide is the same.

## 4. First Boot and SSH

1. Insert the card into the Pi and power it on.
2. Wait 2-3 minutes. The first boot resizes the filesystem and reboots once.
3. From your computer:
   ```
   ping raspicam.local
   ssh <your-user>@raspicam.local
   ```
   If `.local` names do not resolve on your network, find the Pi's IP in your
   router's DHCP client list and use `ssh <your-user>@<ip>`.

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

Options:

```
sudo ./scripts/setup-pi.sh --help
  --user NAME               user that runs the UI (default: the sudo user)
  --go-version X.Y.Z        Go toolchain version
  --mediamtx-version vX.Y.Z MediaMTX release
  --ui-addr ADDR            UI listen address (default :8080)
  --skip-apt                skip apt-get update/install
```

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
newer MediaMTX, pass `--mediamtx-version`.

## 8. Manual Setup (What the Script Does)

Run these on the Pi as your normal user (commands use `sudo` where needed).
Replace `pi` with your username. For 64-bit OS use the `arm64` downloads
instead of `armv6l` (Go) / `armv7` (MediaMTX).

### 8.1 Packages

```
sudo apt install -y --no-install-recommends ca-certificates curl git iw rpicam-apps-lite
```

- `iw`: WiFi link details in the UI.
- `vcgencmd` (temperature, volts, throttling) ships with Raspberry Pi OS.
  Your user must be in the `video` group (default for the first user):
  `sudo usermod -aG video pi`.

### 8.2 Go Toolchain

The Debian `golang` package can be older than `go.mod` requires, so use the
official tarball. On 32-bit OS the right one is `armv6l`:

```
cd /tmp
curl -fsSLO https://dl.google.com/go/go1.27.4.linux-armv6l.tar.gz
echo "$(curl -fsSL https://dl.google.com/go/go1.27.4.linux-armv6l.tar.gz.sha256)  go1.27.4.linux-armv6l.tar.gz" | sha256sum -c
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.27.4.linux-armv6l.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/golang.sh
source /etc/profile.d/golang.sh
go version
```

### 8.3 MediaMTX

MediaMTX embeds its own libcamera, so nothing else is needed for the camera.

```
cd /tmp
V=v1.21.3
curl -fsSLO https://github.com/bluenviron/mediamtx/releases/download/$V/mediamtx_${V}_linux_armv7.tar.gz
mkdir mediamtx && tar -C mediamtx -xzf mediamtx_${V}_linux_armv7.tar.gz
sudo install -m 0755 mediamtx/mediamtx /usr/local/bin/mediamtx
mediamtx --version
```

### 8.4 MediaMTX Config

The config lives in its own directory, owned by the UI user:

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
2. Replace everything under `paths:` (the last section of the file) with:
   ```yaml
   paths:
     cam:
       source: rpiCamera
   ```
   The path must be named `cam`; the UI edits the `rpiCamera*` keys under
   `paths.cam`.

The default auth settings already allow API access from `127.0.0.1`.

### 8.5 Build and Install the UI

```
cd ~/RaspiCam
GOFLAGS=-p=1 go build -trimpath -ldflags "-s -w" -o raspicam-ui ./cmd/ui
sudo install -m 0755 raspicam-ui /usr/local/bin/raspicam-ui
rm raspicam-ui
```

`-p=1` limits parallel compilation so the build fits in 512 MB of RAM.

Alternative: cross-compile on your computer and copy the binary over (much
faster than building on the Zero):

```
GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "-s -w" -o raspicam-ui ./cmd/ui
scp raspicam-ui pi@raspicam.local:/tmp/
ssh pi@raspicam.local sudo install -m 0755 /tmp/raspicam-ui /usr/local/bin/raspicam-ui
```

(Use `GOARCH=arm64` and drop `GOARM` for a 64-bit OS.)

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

- `mediamtx.service` runs MediaMTX as root (as in the upstream start-on-boot
  guide) with the config path above.
- The drop-in sets the UI user and points it at the config. The repo unit
  defaults to `User=pi`; the drop-in overrides it.

Boot ordering (see `SYSTEM.md`): Raspberry Pi OS uses NetworkManager.

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
`http://raspicam.local:8889/cam` to watch the stream. After a reboot, both
services should be `active` within about 30 seconds; check boot delays with
`systemd-analyze blame | head`.

## 10. Troubleshooting

- **No camera detected** (`rpicam-hello --list-cameras` lists nothing):
  power off, reseat both ends of the ribbon cable (contacts towards the
  boards), make sure it is a Zero cable. Check `/boot/firmware/config.txt`
  still has `camera_auto_detect=1`. `dmesg | grep -i imx` shows sensor
  probing.
- **MediaMTX says the camera is busy**: another process (for example a
  running `rpicam-hello`) holds it. Stop it, then
  `sudo systemctl restart mediamtx`.
- **UI shows "save error" for camera config**: the UI user cannot write
  `/usr/local/etc/mediamtx/`. Fix with
  `sudo chown -R pi:pi /usr/local/etc/mediamtx`.
- **UI shows no temperature/volts**: `vcgencmd` is missing or the user is not
  in the `video` group (`groups pi`). Re-login or restart the service after
  adding the group.
- **MediaMTX status "unknown" in the UI**: check `api: true` and
  `apiAddress: 127.0.0.1:9997` in the config, and that
  `curl http://127.0.0.1:9997/v3/paths/list` works on the Pi.
- **Go build killed / very slow**: the Zero ran out of memory. Make sure
  `GOFLAGS=-p=1` is set (the script does this), close other programs, or
  cross-compile (section 8.5).
- **Boot takes ~2 extra minutes**: `systemd-networkd-wait-online.service` is
  enabled; disable it (section 8.6).
- **Undervoltage / throttling in the UI**: use a better power supply and a
  short, thick USB cable.

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
