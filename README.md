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

Alternatively install luffy on the USB stick that already holds the share
folder, or build a custom image with more flash.

## Install on a router

```sh
# Copy the binary and the service file.
scp dist/luffy-linux-mips-softfloat root@192.168.1.1:/usr/bin/luffy
scp init.d/luffy.openwrt root@192.168.1.1:/etc/init.d/luffy

# On the router:
chmod +x /usr/bin/luffy /etc/init.d/luffy
/etc/init.d/luffy enable
/etc/init.d/luffy start
```

Luffy reads the same `/opt/piratebox/conf/piratebox.conf` as the original
release. Existing installations need no configuration changes: the first
start creates missing folders, copies the default web content from the
binary itself and writes the generated `hosts`, `dnsmasq`, `radvd`,
`lighttpd/env` and `avahi` files.

On a systemd host use `init.d/luffy.service` instead.

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

## Licence

GPLv3, inherited from the original PirateBox scripts. See `LICENSE`.
