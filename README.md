# connect-slicer-bridge

Send prints from PrusaSlicer 2.x (and other slicers that speak the OctoPrint
upload API) to a self-hosted [Prusa Connect](https://github.com/prusa3d/prusa-connect)
server.

PrusaSlicer 2.x can't upload to the local Prusa Connect server: its "Prusa Connect"
host type only talks to Prusa's cloud
([prusa3d/prusa-connect#7](https://github.com/prusa3d/prusa-connect/issues/7)).
This bridge pretends to be OctoPrint. It takes the upload, stores it in Connect,
and has Connect send it to the printer and, if asked, start the print.

```
[PrusaSlicer] > OctoPrint API > [bridge] > REST API > [local Prusa Connect] > [printer]
```

The bridge stores nothing and holds no secrets. The API key you enter in the
slicer is a Prusa Connect API credential, and the bridge passes it through.
Connect decides what that key is allowed to do.

This project is not affiliated with or endorsed by Prusa Research.

If you'd rather not run a separate service,
[connect-octoprint-upload](https://github.com/ZachGoldberg/connect-octoprint-upload)
does the same job as a Prusa Connect extension, which Connect installs and runs
itself.

## Compatibility

| | |
|---|---|
| Prusa Connect | 0.1.0-beta.1 |
| PrusaSlicer | 2.9.x, host type OctoPrint |

Prusa Connect is in beta and its API can change between releases.

## Run it

Run the bridge on any machine that the computer running PrusaSlicer can
reach and that can itself reach the Prusa Connect server. The machine that
runs Connect is the obvious place. Replace `<connect host>` with the address
of your Prusa Connect server, and `8080` with its port if you changed it.

### Docker Compose

Save this as `compose.yaml` in an empty folder and start it with
`docker compose up -d`:

```yaml
services:
  connect-slicer-bridge:
    image: ghcr.io/bada55fa7a55/connect-slicer-bridge:latest
    restart: unless-stopped
    ports:
      - "17481:17481"
    environment:
      CONNECT_URL: http://<connect host>:8080
```

### Docker Compose, next to Prusa Connect

If Prusa Connect runs from Prusa's own compose file on the same machine,
the bridge can talk to it over Docker's network instead of the LAN. Prusa's
compose file names the container `prusa_connect` and the network
`prusa_connect_backend`. Join that network, without editing Prusa's files:

```yaml
services:
  connect-slicer-bridge:
    image: ghcr.io/bada55fa7a55/connect-slicer-bridge:latest
    restart: unless-stopped
    ports:
      - "17481:17481"
    environment:
      CONNECT_URL: http://prusa_connect:8080
    networks:
      - prusa_connect_backend

networks:
  prusa_connect_backend:
    external: true
```

Start Prusa Connect first, because it creates the network. If you changed
Connect's port in its `config.yaml`, use that port here too: it's the port
inside the container, not the published one.

### Docker

```sh
docker run -d --name connect-slicer-bridge --restart unless-stopped \
  -p 17481:17481 \
  -e CONNECT_URL=http://<connect host>:8080 \
  ghcr.io/bada55fa7a55/connect-slicer-bridge:latest
```

### Without Docker

Download the binary for your system from
[Releases](https://github.com/bada55fa7a55/connect-slicer-bridge/releases)
and check it against `SHA256SUMS`. There are builds for Linux (amd64, arm64,
armv7 for 32-bit Raspberry Pi OS), macOS and Windows. Then run it:

```sh
chmod +x connect-slicer-bridge_linux_amd64
./connect-slicer-bridge_linux_amd64 -connect http://<connect host>:8080
```

The macOS builds are not signed. macOS blocks them until you run
`xattr -d com.apple.quarantine <file>`.

Or build it with Go:

```sh
go install github.com/bada55fa7a55/connect-slicer-bridge@latest
connect-slicer-bridge -connect http://<connect host>:8080
```

### Check that it runs

`http://<bridge host>:17481/healthz` answers `204 No Content`. The log
prints `listening` with the address and the Connect URL at startup.

## Configuration

Every setting is a command-line flag or an environment variable. Only the
Connect URL is required.

| flag | env | default | |
|---|---|---|---|
| `-connect` | `CONNECT_URL` | | Base URL of the Prusa Connect server. Required |
| `-listen` | `LISTEN_ADDR` | `:17481` | Address to listen on |
| `-storage` | `CONNECT_STORAGE` | `local` | Connect storage that receives uploads |
| `-printer-storage` | `PRINTER_STORAGE` | `usb` | Storage on the printer that files go to |
| `-keep` | `KEEP_FOLDER` | | Keep uploads in this folder of the Connect storage. Without it, uploads are temporary copies that Connect removes |

## Set up PrusaSlicer

1. In Prusa Connect, open **Settings → API credentials → New credential**
   and choose the scope **operate**. Copy the token.
2. In PrusaSlicer, add a physical printer:
   - **Host type:** OctoPrint
   - **Hostname, IP or URL:** `http://<bridge host>:17481/<printer uuid>`
   - **API Key / Password:** the token
3. Click **Test**.

To find the printer UUID, open the printer in Prusa Connect. It's the last
part of the address in the browser,
`http://<connect host>:8080/printers/<printer uuid>/…`, and it's also listed
as **Printer UUID** on the printer's **Printer** tab. With a wrong UUID,
**Test** fails and lists every printer the key can see, with its UUID.
Add one physical printer per printer.

**Upload** puts the file on the printer's USB drive. **Upload and Print**
also starts it. Uploading a file whose name is already on the drive replaces
the old one. PrusaSlicer's upload queue shows Connect's error if the printer
refuses, for example because it is already printing.

## Security

The bridge speaks plain HTTP, just like Prusa Connect's default setup. Run
it only on a network you trust. The token travels in the `X-Api-Key` header.

## Vibe coded

This project was vibe coded: an AI assistant wrote the code, and a human
pointed it at the problem and checked that the result works. It has tests,
it ran against Prusa Connect's printer simulator, and it printed on a real
MK4S. A human has not reviewed every line, though. Read the code before you
trust it with your printer. It's short.

## License

MIT
