# Luffy

Luffy is a Go rewrite of the PirateBox server. It replaces the Python and
shell components of the original release with a single static binary, while
keeping the existing configuration, data formats and web interface.

The original project targeted devices like the TP-Link TL-MR3020 (Atheros
AR9331, 32-bit big-endian MIPS, 4 MB flash, 32 MB RAM). A static Go binary
with a soft-float MIPS build fits that target and removes the Python, Perl
and lighttpd dependencies.

## What it replaces

| Original | Luffy |
| --- | --- |
| `lighttpd` + `conf/lighttpd/*` | `internal/web` (net/http) |
| `python_lib/psogen.py` | `internal/shoutbox` |
| `python_lib/messages.py`, `broadcast.py`, `discover.py` | `internal/shoutbox` |
| `www/cgi-bin/*.py` | `internal/web` handlers |
| `bin/droopy` | `internal/upload` |
| `src/forest.py` (optional Python forum) | `internal/forum` |
| Kareha imageboard (Perl, downloaded at install) | not ported, see below |
| `python_lib/diskusage.py`, `psutil.py` | `internal/diskusage` |
| `bin/station_cnt.sh` | `internal/stations` |
| `bin/timesave.sh` | `internal/timesave` |
| `bin/generate_config_files.sh` | `internal/sysconfig` |
| `bin/firewall.sh` | `internal/service` |
| `bin/piratebox_setup_wlan.sh` | `internal/service` |
| `init.d/piratebox` | `internal/service` |
| `bin/hooks/*.sh` | `internal/hooks` (the same scripts still run) |
| `bin/distribute_files.sh`, `delete_empty.sh`, `flush_dnsmasq.sh` | `internal/service` |

## Build

```sh
make build          # host binary for development
make test           # run the test suite
make dist           # static binaries for mips, mipsle, arm, arm64, amd64
```

For the TL-MR3020:

```sh
make mips
# dist/luffy-linux-mips-softfloat
```

The MIPS build is static, soft-float and stripped. It runs on OpenWrt
`mips_24kc` systems without any runtime dependencies.

The uncompressed MIPS binary is about 7.9 MB. A TL-MR3020 only has 4 MB of
flash, so compress the router builds with UPX (verified at 1.9 MB):

```sh
make upx    # requires the upx tool
```

If you would rather not build anything, every release ships the same five
binaries. See the installation guide below.

## Installation

### Which file do I need?

Download the right binary from the
[releases page](https://github.com/et0and/PirateBoxScripts_Webserver/releases),
or build it yourself with `make dist`.

| Device | File | Notes |
| --- | --- | --- |
| TP-Link TL-MR3020, TL-WR703N, most OpenWrt routers | `luffy-linux-mips-softfloat` | Big-endian MIPS 24Kc, no FPU. UPX compressed to fit 4 MB flash. |
| Little-endian MIPS routers | `luffy-linux-mipsle-softfloat` | Check with `uname -m` on the router: it prints `mips` or `mipsel`. |
| Raspberry Pi 1, Zero, Zero W | `luffy-linux-armv6` | Raspberry Pi OS 32-bit. |
| Raspberry Pi 2, 3, 4, 5 | `luffy-linux-arm64` | Raspberry Pi OS 64-bit. |
| Any x86-64 Linux PC or laptop | `luffy-linux-amd64` | Good for a test run before touching the router. |

Not sure which one? Run `uname -m` on the device. `mips` means
`luffy-linux-mips-softfloat`, `mipsel` means `luffy-linux-mipsle-softfloat`,
`armv6l` means `luffy-linux-armv6` and `aarch64` means `luffy-linux-arm64`.

### TP-Link TL-MR3020 (OpenWrt)

The TL-MR3020 has 4 MB of flash and 32 MB of RAM. The UPX compressed binary
is about 1.9 MB, so it fits in flash, but the shared files belong on a USB
stick.

On OpenWrt, let OpenWrt keep managing the WiFi and the interfaces, and let
luffy serve the web interface, the chat and the captive portal DNS. This is
the same split the original PirateBox image used on OpenWrt. Luffy only
takes over DHCP and DNS, so its `address=/#/` rule can send every client to
the start page.

**1. Get the binary onto your computer.**

```sh
# Either download it from the releases page, or build it:
cd luffy
make upx
# dist/luffy-linux-mips-softfloat
```

**2. Connect to the router and check the architecture.**

```sh
ssh root@192.168.1.1
uname -m          # must print: mips
```

If SSH is not enabled, connect with an Ethernet cable, browse to
`http://192.168.1.1`, and enable Dropbear under System, Administration.

**3. Create the WiFi access point in LuCI.**

Browse to `http://192.168.1.1`, then:

1. Network, Wireless, Edit on the radio.
2. Set Mode to `Access Point`.
3. Set ESSID to `PirateBox - Share Freely`.
4. Set Encryption to `No Encryption` (an open network, as PirateBox
   intends).
5. Set Channel to `1`.
6. Save and Apply.

**4. Move the LAN address to the PirateBox subnet.**

Still in LuCI, go to Network, Interfaces, LAN, Edit:

1. Set IPv4 address to `192.168.77.1`.
2. Set IPv4 netmask to `255.255.255.0`.
3. Under DHCP Server, tick `Ignore interface` so OpenWrt stops handing out
   addresses. Luffy's dnsmasq does that from here on.
4. Save and Apply.

Reconnect at `http://192.168.77.1`.

**5. Disable OpenWrt's own dnsmasq.**

Two DHCP servers on one interface will fight. Luffy runs its own, with the
captive portal redirect:

```sh
/etc/init.d/dnsmasq stop
/etc/init.d/dnsmasq disable
```

**6. Prepare the USB stick for the shared files.**

The MR3020 cannot hold the shared folder in flash. Format a USB stick as
ext4 or FAT32, plug it in, and mount it at `/opt/piratebox`:

```sh
mkdir -p /opt/piratebox
ls /dev/sd*                    # usually /dev/sda1
mount /dev/sda1 /opt/piratebox
```

To mount it automatically on every boot, add it to `/etc/fstab`:

```sh
# ext4:
echo "/dev/sda1 /opt/piratebox ext4 rw,noatime 0 0" >> /etc/fstab
# or FAT32 (umask=000 lets uploads be written):
echo "/dev/sda1 /opt/piratebox vfat rw,noatime,umask=000 0 0" >> /etc/fstab
```

**7. Copy luffy and the init script to the router.**

Run this from your computer, in the `luffy` directory:

```sh
scp dist/luffy-linux-mips-softfloat root@192.168.77.1:/usr/bin/luffy
scp init.d/luffy.openwrt root@192.168.77.1:/etc/init.d/luffy
```

**8. Create the configuration.**

Back on the router:

```sh
mkdir -p /opt/piratebox/conf
cat > /opt/piratebox/conf/piratebox.conf <<'EOF'
PIRATEBOX_FOLDER="/opt/piratebox"
WWW_FOLDER="$PIRATEBOX_FOLDER/www"
WWW_CONTENT="$PIRATEBOX_FOLDER/share/content"
SHARE_FOLDER="$PIRATEBOX_FOLDER/share"
UPLOADFOLDER="$SHARE_FOLDER/Shared"

# OpenWrt manages the radio and the interface, so luffy must not.
DO_IFCONFIG="no"
USE_APN="no"

# Luffy's dnsmasq hands out addresses on the LAN bridge.
USE_DNSMASQ="yes"
DNSMASQ_INTERFACE="br-lan"
NET=192.168.77
IP_SHORT=1
NETMASK=255.255.255.0

# The name clients see in the browser.
HOST="piratebox.lan"

# Set this to the access point interface from `iw dev`, for the client
# counter. It is often wlan0, and phy0-ap0 on newer OpenWrt releases.
INTERFACE="wlan0"
EOF
```

This is the same file format as the original PirateBox release. Everything
you leave out falls back to a built-in default, so you only list what you
want to change. If you are upgrading an existing PirateBox, skip this step:
luffy reads your current `/opt/piratebox/conf/piratebox.conf` unchanged.

Check the interface name, then fix `INTERFACE` if it differs:

```sh
iw dev | grep Interface
```

**9. Enable and start.**

```sh
chmod +x /usr/bin/luffy /etc/init.d/luffy
/etc/init.d/luffy enable
/etc/init.d/luffy start
```

The first start creates the folders, writes the web content from inside the
binary, generates the dnsmasq configuration and starts the web server and
the DHCP server.

**10. Connect and check.**

Join the `PirateBox - Share Freely` WiFi network and open
`http://piratebox.lan` or `http://192.168.77.1`. You should see the start
page with the chat box and the upload area. Opening any other address also
lands on the start page, which is the captive portal behaviour.

**11. Useful commands.**

```sh
/etc/init.d/luffy status       # is it running
/etc/init.d/luffy restart      # after a configuration change
logread | tail -50             # recent log lines
luffy --config /opt/piratebox/conf/piratebox.conf status
```

Two notes for OpenWrt:

* Clients with a hardcoded DNS server (for example `8.8.8.8`) bypass
  dnsmasq. The original project handled those with iptables rules. To do
  the same, set `FIREWALL_FETCH_DNS="yes"` in the configuration and install
  the compatibility package:

  ```sh
  opkg install iptables iptables-mod-nat-extra
  ```

* To change the WiFi password, edit the network in LuCI. Luffy never
  touches `hostapd.conf` on OpenWrt.

### Other OpenWrt routers

Follow the TL-MR3020 steps with these changes:

* Check `uname -m` and pick `mips` or `mipsel` accordingly. Routers with an
  ARM or ARM64 CPU use the matching `armv6` or `arm64` build.
* Routers with 8 MB or more flash can run the uncompressed binary, but the
  UPX build works everywhere and starts faster from flash.
* If your router has enough space for the share folder in flash, point
  `SHARE_FOLDER` at a directory on the internal storage instead of a USB
  stick.
* On OpenWrt 22.03 and later the firewall is `nftables`. Leave
  `FIREWALL_FETCH_DNS="no"`; the dnsmasq `address=/#/` rule still redirects
  every client that uses the DHCP supplied DNS server.
* A router with more than one radio can keep one radio for a normal
  uplink and use the other for the PirateBox access point.

### Raspberry Pi

**1. Get the binary.**

```sh
# 64-bit Raspberry Pi OS:
make arm64     # dist/luffy-linux-arm64
# 32-bit Raspberry Pi OS on a Pi 1, Zero or Zero W:
make arm       # dist/luffy-linux-armv6
```

**2. Copy the files.**

```sh
scp dist/luffy-linux-arm64 pi@raspberrypi.local:/tmp/luffy
scp init.d/luffy.service pi@raspberrypi.local:/tmp/luffy.service
```

**3. Install and configure.**

```sh
ssh pi@raspberrypi.local
sudo apt update
sudo apt install hostapd dnsmasq iw
sudo install -m 0755 /tmp/luffy /usr/bin/luffy
sudo install -m 0644 /tmp/luffy.service /etc/systemd/system/luffy.service
sudo mkdir -p /opt/piratebox/conf
sudo tee /opt/piratebox/conf/piratebox.conf >/dev/null <<'EOF'
PIRATEBOX_FOLDER="/opt/piratebox"
WWW_FOLDER="$PIRATEBOX_FOLDER/www"
WWW_CONTENT="$PIRATEBOX_FOLDER/share/content"
SHARE_FOLDER="$PIRATEBOX_FOLDER/share"
UPLOADFOLDER="$SHARE_FOLDER/Shared"
INTERFACE="wlan0"
DNSMASQ_INTERFACE="wlan0"
NET=192.168.77
IP_SHORT=1
HOST="piratebox.lan"
USE_APN="yes"
USE_DNSMASQ="yes"
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now luffy
```

**4. Stop the default services that would fight luffy.**

Raspberry Pi OS runs its own `hostapd` and `dnsmasq` units. Disable them so
only luffy manages them:

```sh
sudo systemctl disable --now hostapd dnsmasq
```

The Pi needs a WiFi adapter that supports access point mode. The built-in
adapter on a Pi 3 and later works; on a Pi 1 or Zero use a USB adapter.

### Linux desktop or laptop (test run first)

This is the quickest way to see luffy working before touching a router. It
runs the web interface only, and does not change your network:

```sh
make build
mkdir -p /tmp/luffy-demo
cat > /tmp/luffy-demo/piratebox.conf <<'EOF'
PIRATEBOX_FOLDER="/tmp/luffy-demo"
WWW_FOLDER="$PIRATEBOX_FOLDER/www"
WWW_CONTENT="$PIRATEBOX_FOLDER/share/content"
SHARE_FOLDER="$PIRATEBOX_FOLDER/share"
UPLOADFOLDER="$SHARE_FOLDER/Shared"
DO_IFCONFIG="no"
USE_APN="no"
USE_DNSMASQ="no"
HTTP_PORT="8080"
DROOPY_PORT="8081"
EOF
./luffy --config /tmp/luffy-demo/piratebox.conf setup
./luffy --config /tmp/luffy-demo/piratebox.conf serve
```

Now open `http://localhost:8080`. Chat, the forum, uploads and the file
listing all work. Press Ctrl+C to stop.

To try the full start path, including interface setup, use `luffy start`
instead of `serve`. That needs root and a spare wireless interface.

### Upgrading an existing PirateBox

Luffy is a drop-in replacement. It reads the existing
`/opt/piratebox/conf/piratebox.conf`, keeps the chat history in `data.pso`,
keeps the forum files in `share/forumspace` and runs the same
`bin/hooks/*.sh` scripts.

```sh
# Stop the old services.
/etc/init.d/piratebox stop

# Remove lighttpd and droopy; luffy replaces both.
opkg remove lighttpd 2>/dev/null

# Install luffy, keeping /opt/piratebox as it is.
scp dist/luffy-linux-mips-softfloat root@192.168.1.1:/usr/bin/luffy
scp init.d/luffy.openwrt root@192.168.1.1:/etc/init.d/luffy
ssh root@192.168.1.1 'chmod +x /usr/bin/luffy /etc/init.d/luffy && \
  /etc/init.d/luffy enable && /etc/init.d/luffy start'
```

Your chat history, uploaded files and forum threads are untouched. The first
start notices the existing installation and only fills in what is missing.

## Commands

```
luffy start                network setup, services and web server
luffy serve                web server only, no network changes
luffy stop                 stop services and helpers
luffy setup                install folders, links and generated configs
luffy status               print the effective configuration
luffy shoutbox post "..."  append a chat message from the shell
luffy shoutbox generate    regenerate chat_content.html
luffy timesave save        store the clock for RTC-less devices
luffy timesave recover     restore the stored clock
luffy stations             print the connected client count
luffy hostname mybox.lan   change the visible hostname
luffy flush-dns            signal dnsmasq to reload its hosts files
luffy version              print the version
```

Global flags: `--config PATH`, `--log-level debug|info|warn|error`,
`--dry-run`.

## Compatibility

* `piratebox.conf` keeps the shell syntax, including `$VARIABLE` expansion,
  `$(cat file)` command substitution and `. file` includes. The parser is in
  `internal/config`.
* The chat file (`data.pso`) and the generated HTML frame use the same
  markup, so existing histories render unchanged.
* The forum reads and writes the `forest.py` flat-file format, including
  Python 2 `string_escape` encoded bodies. Old boards keep working.
* The global chat UDP protocol is unchanged: `piratebox;sb;01;<name>;<base64>`
  on port 12556.
* Uploads keep droopy behaviour: multiple files, collision numbering
  (`name-1.ext`), `index.*` blocking and the same translations.
* Captive portal probes are answered: `/generate_204`, `/ncsi.txt` and
  `/library/test/success.html`.
* The `bin/hooks/*.sh` scripts still run with the configuration path as the
  first argument.

## What is not ported

* The Kareha imageboard is a third-party Perl CGI script that the installer
  downloaded at runtime. Luffy replaces it with the built-in Go forum
  (`/board/`). The Perl script and its `config.pl` are left in the tree for
  anyone who wants to keep running it behind another web server.
* The Raspberry Pi helper scripts (`rpi/bin/*`) handle I2C real time clocks
  and SD card partitioning. They are hardware specific and stay as they are.
* `src/kareha.pl` and `src/kareha_img_config.pl` are not used by luffy.

## Layout

```
luffy/
  cmd/luffy/          command line entry point
  internal/config/    piratebox.conf parser and typed configuration
  internal/web/       HTTP server and routes
  internal/shoutbox/  chat, wire format and UDP listener
  internal/forum/     flat-file board
  internal/upload/    droopy compatible uploads
  internal/dirlist/   lighttpd style directory listings
  internal/diskusage/ disk usage fragment
  internal/stations/  connected client counter
  internal/timesave/  clock persistence
  internal/sysconfig/ generated configuration files
  internal/service/   lifecycle, network setup and maintenance
  internal/hooks/     user hook scripts
  internal/proc/      command runner (fakeable in tests)
  assets/             embedded default www_content
  init.d/             OpenWrt and systemd service files
```

## Development

```sh
go test ./...                 # unit and integration tests
go test -race ./...           # race detector
go test -cover ./...          # coverage per package
go vet ./...
```

Tests use a fake command runner (`internal/proc.Fake`) so network and
service behaviour is verified without root access or real interfaces.
Parity tests pin the byte-level output of the chat line, wire format and
forum escaping to values captured from the Python implementation.

The suite covers 89% of statements and the CI enforces an 85% floor.

## Continuous integration and releases

`.github/workflows/luffy-ci.yml` runs on every change under `luffy/`:

* `gofmt` check and `go vet`
* the full test suite on Linux and macOS
* the test suite under the race detector on Linux
* total coverage, published to the job summary with an 85% floor
* cross-compilation for mips, mipsle, armv6, arm64 and amd64

The repeated steps live in composite actions under `.github/actions/` so
the CI and release workflows share one definition of each step:

| Action | Purpose |
| --- | --- |
| `setup` | Installs the Go toolchain pinned by `go.mod`, with caching. |
| `verify` | `gofmt` check, `go vet`, tests, optional race detector. |
| `build-target` | Cross-compiles one static target with the release flags. |

Because the release workflow calls the same `verify` and `build-target`
actions, a published binary is always built the way CI tested it.

Every third-party action is pinned to a commit SHA rather than a moving
tag, with the version in a trailing comment. Dependabot
(`.github/dependabot.yml`) opens a grouped pull request when a newer
release appears, so the pins stay current without losing the guarantee
that a tag cannot be moved under us.

Releases are cut by pushing a tag:

```sh
git tag luffy-v1.0.0
git push origin luffy-v1.0.0
```

`.github/workflows/luffy-release.yml` then verifies the tag, builds all
five architectures, compresses the MIPS builds with UPX, writes
`SHA256SUMS` and publishes a GitHub release with the binaries attached.
The `luffy-v` prefix keeps these tags separate from the historical
PirateBox tags.

## Credits

Luffy is a rewrite of the PirateBox server, which was built by other people
first. The configuration format, the chat protocol, the web interface and
the data layouts all come from their work.

* **David Darts** — the original PirateBox idea.
* **Matthias Strubel** — the script collection this port follows, and the
  author of most of the shell and Python that luffy replaces.
* **PirateBox-Dev** — the project that maintained the scripts.
* **David Brooks** — the interface design.

Luffy also stands in for software that it does not contain:

* **Droopy** by Stéphane Pelletier — the file upload interface.
* **forest.py** by Nelis Franken — the flat-file forum whose format luffy
  reads and writes.
* **Kareha** by Wakaba — the imageboard, which is not ported.

Thank you to the contributors listed in the original project's `AUTHORS`
file, whose work shaped the behaviour luffy reproduces.

The original project lives at
<https://github.com/PirateBox-Dev/PirateBoxScripts_Webserver>.

## Licence

GPLv3, inherited from the original PirateBox scripts. See `LICENSE`.
